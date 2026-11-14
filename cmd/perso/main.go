package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/perso/perso-mind/internal/analytics"
	"github.com/perso/perso-mind/internal/api"
	"github.com/perso/perso-mind/internal/api/handlers"
	"github.com/perso/perso-mind/internal/config"
	"github.com/perso/perso-mind/internal/conversation"
	"github.com/perso/perso-mind/internal/llm"
	"github.com/perso/perso-mind/internal/matching"
	"github.com/perso/perso-mind/internal/queue"
	"github.com/perso/perso-mind/internal/storage/mongodb"
	"github.com/perso/perso-mind/internal/storage/postgres"
	"github.com/perso/perso-mind/internal/telegram"
	"github.com/perso/perso-mind/internal/worker"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	setupLogger(cfg.LogLevel)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cmd := "all"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	slog.Info("starting Perso Marketplace Platform", "command", cmd, "env", cfg.Environment)

	// Connect PostgreSQL
	pgDB, err := postgres.Connect(ctx, cfg.PostgresDSN())
	if err != nil {
		slog.Error("fatal postgres connection error", "error", err)
		os.Exit(1)
	}
	defer pgDB.Close()

	// Automatically run migrations
	if err := pgDB.Migrate(ctx); err != nil {
		slog.Error("fatal postgres migration error", "error", err)
		os.Exit(1)
	}

	if cmd == "migrate" {
		slog.Info("migration completed successfully. Exiting.")
		return
	}

	// Connect MongoDB
	mongoDB, err := mongodb.Connect(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		slog.Error("fatal mongodb connection error", "error", err)
		os.Exit(1)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = mongoDB.Close(closeCtx)
	}()

	// Connect Redis
	redisClient, err := queue.Connect(ctx, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		slog.Error("fatal redis connection error", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	// Ensure Redis Consumer Group exists
	if err := redisClient.EnsureConsumerGroup(ctx, cfg.RedisStreamKey, cfg.RedisConsumerGroup); err != nil {
		slog.Warn("ensure redis consumer group error", "error", err)
	}

	// Repositories
	userRepo := postgres.NewUserRepo(pgDB)
	productRepo := postgres.NewProductRepo(pgDB)
	interestRepo := postgres.NewInterestRepo(pgDB)
	watchSourceRepo := postgres.NewWatchSourceRepo(pgDB)
	telegramSourceRepo := postgres.NewTelegramSourceRepo(pgDB)
	sellerProfileRepo := postgres.NewSellerProfileRepo(pgDB)
	matchResultRepo := postgres.NewMatchResultRepo(pgDB)
	offerRepo := postgres.NewOfferRepo(pgDB)
	analyticsRepo := postgres.NewAnalyticsRepo(pgDB)
	convRepo := postgres.NewConversationRepo(pgDB)
	auditRepo := postgres.NewAuditRepo(pgDB)

	messagesRepo := mongodb.NewMessagesRepo(mongoDB)
	updatesRepo := mongodb.NewUpdatesRepo(mongoDB)
	extractionRepo := mongodb.NewExtractionRepo(mongoDB)
	llmExtractionsRepo := mongodb.NewLLMExtractionsRepo(mongoDB)
	mongoConvRepo := mongodb.NewConversationRepo(mongoDB)
	jobsRepo := mongodb.NewJobsRepo(mongoDB)

	// Seed or ensure default demo buyer user exists
	_, _ = userRepo.GetOrCreateDefaultUser(ctx)

	// Queue Producer
	producer := queue.NewProducer(redisClient, cfg.RedisStreamKey)

	// Matching and Analytics engines
	matcher := matching.NewMatcher(productRepo)
	analyticsEngine := analytics.NewEngine(offerRepo, productRepo, analyticsRepo)

	// LLM Provider
	var llmProvider llm.Provider
	if cfg.LLMEnabled && cfg.GeminiAPIKey != "" {
		slog.Info("initializing gemini llm provider", "model", cfg.GeminiModel)
		llmProvider = llm.NewGeminiProvider(cfg.GeminiAPIKey, cfg.GeminiModel, cfg.LLMTimeoutSecs, cfg.LLMMaxRetries)
	} else {
		slog.Info("using mock llm provider (llm disabled by default for privacy compliance)")
		llmProvider = llm.NewMockProvider()
	}

	// Collector & Telegram
	collector, err := telegram.NewCollector(cfg, watchSourceRepo, telegramSourceRepo, messagesRepo, updatesRepo, producer)
	if err != nil {
		slog.Error("fatal collector init error", "error", err)
		os.Exit(1)
	}

	var tgClient telegram.TelegramClient
	if collector.MockClient() != nil {
		tgClient = collector.MockClient()
	}

	// Conversation Manager
	convManager := conversation.NewManager(cfg, convRepo, mongoConvRepo, auditRepo, tgClient, llmProvider)
	convManager.SetSellerRepo(sellerProfileRepo)

	// Worker Processor
	processor := worker.NewMessageProcessor(
		cfg, messagesRepo, extractionRepo, llmExtractionsRepo, jobsRepo, matcher,
		offerRepo, productRepo, interestRepo, sellerProfileRepo, matchResultRepo,
		convManager, analyticsEngine, llmProvider,
	)

	// Dispatch commands
	switch cmd {
	case "api":
		runAPIServer(ctx, cfg, pgDB, mongoDB, redisClient, interestRepo, productRepo, watchSourceRepo, telegramSourceRepo, matchResultRepo, collector.Resolver(), offerRepo, convRepo, mongoConvRepo, convManager, analyticsRepo, messagesRepo, analyticsEngine)
	case "collector":
		runCollector(ctx, collector)
	case "worker":
		runWorker(ctx, cfg, redisClient, processor)
	case "all":
		go runCollector(ctx, collector)
		go runWorker(ctx, cfg, redisClient, processor)
		runAPIServer(ctx, cfg, pgDB, mongoDB, redisClient, interestRepo, productRepo, watchSourceRepo, telegramSourceRepo, matchResultRepo, collector.Resolver(), offerRepo, convRepo, mongoConvRepo, convManager, analyticsRepo, messagesRepo, analyticsEngine)
	default:
		slog.Error("unknown command", "command", cmd)
		os.Exit(1)
	}
}

func runAPIServer(
	ctx context.Context,
	cfg *config.Config,
	pgDB *postgres.DB,
	mongoDB *mongodb.DB,
	redisClient *queue.Client,
	interestRepo *postgres.InterestRepo,
	productRepo *postgres.ProductRepo,
	watchSourceRepo *postgres.WatchSourceRepo,
	telegramSourceRepo *postgres.TelegramSourceRepo,
	matchResultRepo *postgres.MatchResultRepo,
	resolver *telegram.Resolver,
	offerRepo *postgres.OfferRepo,
	convRepo *postgres.ConversationRepo,
	mongoConvRepo *mongodb.ConversationRepo,
	convManager *conversation.Manager,
	analyticsRepo *postgres.AnalyticsRepo,
	messagesRepo *mongodb.MessagesRepo,
	analyticsEngine *analytics.Engine,
) {
	healthH := handlers.NewHealthHandler(pgDB, mongoDB, redisClient)
	interestH := handlers.NewInterestHandler(interestRepo, productRepo)
	watchSourceH := handlers.NewWatchSourceHandler(watchSourceRepo)
	telegramSourceH := handlers.NewTelegramSourceHandler(telegramSourceRepo, resolver)
	productH := handlers.NewProductHandler(productRepo, analyticsEngine)
	offerH := handlers.NewOfferHandler(offerRepo, matchResultRepo, analyticsEngine)
	convH := handlers.NewConversationHandler(convRepo, mongoConvRepo, convManager)
	analyticsH := handlers.NewAnalyticsHandler(analyticsRepo, offerRepo, productRepo, messagesRepo, analyticsEngine)

	router := api.NewRouter(&api.Handlers{
		AllowDemoAuth:         cfg.DemoAuthEnabled,
		HealthHandler:         healthH,
		InterestHandler:       interestH,
		WatchSourceHandler:    watchSourceH,
		TelegramSourceHandler: telegramSourceH,
		ProductHandler:        productH,
		OfferHandler:          offerH,
		ConversationHandler:   convH,
		AnalyticsHandler:      analyticsH,
	})

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("HTTP REST API server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down HTTP REST API server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	slog.Info("HTTP server stopped gracefully")
}

func runCollector(ctx context.Context, collector *telegram.Collector) {
	slog.Info("starting telegram ingestion service...")
	if err := collector.Start(ctx); err != nil && err != context.Canceled {
		slog.Error("collector encountered fatal error", "error", err)
	}
}

func runWorker(ctx context.Context, cfg *config.Config, redisClient *queue.Client, processor *worker.MessageProcessor) {
	slog.Info("starting message processing worker...", "concurrency", cfg.WorkerConcurrency)

	consumer := queue.NewConsumer(
		redisClient,
		cfg.RedisStreamKey,
		cfg.RedisConsumerGroup,
		cfg.RedisConsumerName,
		cfg.BatchSize,
	)

	if err := consumer.Start(ctx, processor.ProcessStreamMessage); err != nil && err != context.Canceled {
		slog.Error("worker consumer encountered error", "error", err)
	}
}

func setupLogger(levelStr string) {
	var level slog.Level
	switch levelStr {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))
}
