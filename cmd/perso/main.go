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

	"github.com/google/uuid"
	"github.com/gotd/td/tg"
	"github.com/perso/perso-mind/internal/analytics"
	"github.com/perso/perso-mind/internal/api"
	"github.com/perso/perso-mind/internal/api/handlers"
	"github.com/perso/perso-mind/internal/config"
	"github.com/perso/perso-mind/internal/conversation"
	"github.com/perso/perso-mind/internal/identity"
	"github.com/perso/perso-mind/internal/llm"
	"github.com/perso/perso-mind/internal/matching"
	"github.com/perso/perso-mind/internal/queue"
	"github.com/perso/perso-mind/internal/storage/mongodb"
	"github.com/perso/perso-mind/internal/storage/postgres"
	"github.com/perso/perso-mind/internal/telegram"
	"github.com/perso/perso-mind/internal/worker"
	"golang.org/x/sync/errgroup"
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

	if cmd == "login" {
		if err := telegram.Login(ctx, cfg); err != nil {
			slog.Error("Telegram login failed", "error", err)
			os.Exit(1)
		}
		return
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

	identityService := identity.New(pgDB.Pool, cfg.AuthCookieSecure)
	if cmd == "api" || cmd == "all" || cmd == "bootstrap-admin" {
		if err := identityService.Bootstrap(ctx, *cfg.OwnerID(), cfg.BootstrapUsername, cfg.BootstrapPassword); err != nil {
			slog.Error("account bootstrap failed", "error", err)
			os.Exit(1)
		}
		if cmd == "bootstrap-admin" {
			return
		}
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

	if err := mongoDB.ConfigureRetention(ctx, cfg.RetentionDays); err != nil {
		slog.Error("retention configuration", "error", err)
		os.Exit(1)
	}
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

	// Retain the configured operator identity for administrative intake only.
	// Unknown legacy listing/conversation ownership is never adopted at startup.
	owner := cfg.OwnerID()
	if _, err := pgDB.Pool.Exec(ctx, `INSERT INTO users(id,username,role) VALUES($1,$2,'buyer') ON CONFLICT(id) DO NOTHING`, *owner, "operator_"+owner.String()); err != nil {
		slog.Error("operator initialization", "error", err)
		os.Exit(1)
	}

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

	gateway := telegram.NewGatewayClient(cfg.GatewayURL, cfg.GatewayToken)

	// Conversation Manager
	convManager := conversation.NewManager(cfg, convRepo, mongoConvRepo, auditRepo, nil, llmProvider)
	convManager.SetSellerRepo(sellerProfileRepo)

	// Worker Processor
	processor := worker.NewMessageProcessor(
		cfg, messagesRepo, extractionRepo, llmExtractionsRepo, jobsRepo, matcher,
		offerRepo, productRepo, interestRepo, sellerProfileRepo, matchResultRepo,
		convManager, analyticsEngine, llmProvider,
	)

	processor.SetSourceRepo(telegramSourceRepo)
	// Only the gateway constructs or locks an MTProto session.
	var collector *telegram.Collector
	if cmd == "collector" || cmd == "gateway" || cmd == "all" {
		collector, err = telegram.NewCollector(cfg, watchSourceRepo, telegramSourceRepo, messagesRepo, updatesRepo, producer)
		if err != nil {
			slog.Error("gateway initialization", "error", err)
			os.Exit(1)
		}
		collector.SetReplyHandler(func(ctx context.Context, msg *tg.Message) error {
			seller := int64(0)
			if p, ok := msg.FromID.(*tg.PeerUser); ok {
				seller = p.UserID
			}
			if seller == 0 {
				return nil
			}
			conv, err := convRepo.FindActiveBySeller(ctx, seller)
			if err != nil || conv == nil {
				return err
			}
			_, err = convManager.HandleIncomingSellerMessage(postgres.WithInbound(ctx, seller, int64(msg.ID), msg.Message), conv.ID, msg.Message)
			return err
		})
		collector.SetMarketplaceRepo(pgDB)
		collector.SetRetentionHandler(func(ctx context.Context) error { return mongoDB.ConfigureRetention(ctx, cfg.RetentionDays) })
	}
	group, runCtx := errgroup.WithContext(ctx)
	if cmd == "collector" || cmd == "gateway" || cmd == "all" {
		group.Go(func() error { return collector.RunGateway(runCtx) })
	}
	if cmd == "worker" || cmd == "all" {
		group.Go(func() error { return runWorker(runCtx, cfg, redisClient, processor) })
	}
	if cmd == "api" || cmd == "all" {
		group.Go(func() error {
			return runAPIServer(runCtx, cfg, pgDB, mongoDB, redisClient, interestRepo, productRepo, watchSourceRepo, telegramSourceRepo, matchResultRepo, gateway, offerRepo, convRepo, mongoConvRepo, convManager, analyticsRepo, messagesRepo, analyticsEngine)
		})
	}
	if cmd != "api" && cmd != "worker" && cmd != "collector" && cmd != "gateway" && cmd != "all" {
		slog.Error("unknown command", "command", cmd)
		os.Exit(1)
	}
	if err = group.Wait(); err != nil && ctx.Err() == nil {
		slog.Error("service failed", "error", err)
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
	resolver telegram.SourceResolver,
	offerRepo *postgres.OfferRepo,
	convRepo *postgres.ConversationRepo,
	mongoConvRepo *mongodb.ConversationRepo,
	convManager *conversation.Manager,
	analyticsRepo *postgres.AnalyticsRepo,
	messagesRepo *mongodb.MessagesRepo,
	analyticsEngine *analytics.Engine,
) error {
	healthH := handlers.NewHealthHandler(pgDB, mongoDB, redisClient)
	interestH := handlers.NewInterestHandler(interestRepo, productRepo)
	watchSourceH := handlers.NewWatchSourceHandler(watchSourceRepo)
	telegramSourceH := handlers.NewTelegramSourceHandler(telegramSourceRepo, resolver)
	productH := handlers.NewProductHandler(productRepo, analyticsEngine)
	offerH := handlers.NewOfferHandler(offerRepo, matchResultRepo, analyticsEngine)
	convH := handlers.NewConversationHandler(convRepo, mongoConvRepo, convManager)
	analyticsH := handlers.NewAnalyticsHandler(analyticsRepo, offerRepo, productRepo, messagesRepo, analyticsEngine)

	router := api.NewRouter(&api.Handlers{
		Identity: identity.New(pgDB.Pool, cfg.AuthCookieSecure), OutreachEnabled: cfg.OutreachEnabled, AllowDemoAuth: cfg.DemoAuthEnabled, APIToken: cfg.APIToken, OperatorUserID: *cfg.OwnerID(), GatewayHandler: telegram.NewGatewayClient(cfg.GatewayURL, cfg.GatewayToken).StatusHandler(),
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

	errors := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if err == http.ErrServerClosed {
			err = nil
		}
		errors <- err
	}()
	select {
	case err := <-errors:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}
func runWorker(ctx context.Context, cfg *config.Config, client *queue.Client, p *worker.MessageProcessor) error {
	group, runCtx := errgroup.WithContext(ctx)
	for i := 0; i < cfg.WorkerConcurrency; i++ {
		name := fmt.Sprintf("%s-%s-%d", cfg.RedisConsumerName, uuid.NewString(), i)
		group.Go(func() error {
			return queue.NewConsumer(client, cfg.RedisStreamKey, cfg.RedisConsumerGroup, name, cfg.BatchSize).Start(runCtx, p.ProcessStreamMessage)
		})
	}
	return group.Wait()
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
