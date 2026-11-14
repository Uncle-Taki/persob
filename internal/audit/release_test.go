//go:build audit

// This opt-in acceptance suite deliberately exposes unresolved release blockers.
// Run only against the disposable services in audit/2026-11-14/compose.audit.yml.
package audit_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gotd/td/tg"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/perso/perso-mind/internal/analytics"
	"github.com/perso/perso-mind/internal/api/handlers"
	"github.com/perso/perso-mind/internal/api/httputil"
	"github.com/perso/perso-mind/internal/config"
	"github.com/perso/perso-mind/internal/conversation"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/extraction"
	"github.com/perso/perso-mind/internal/llm"
	"github.com/perso/perso-mind/internal/matching"
	"github.com/perso/perso-mind/internal/queue"
	"github.com/perso/perso-mind/internal/storage/mongodb"
	"github.com/perso/perso-mind/internal/storage/postgres"
	"github.com/perso/perso-mind/internal/telegram"
	"github.com/perso/perso-mind/internal/worker"
	"github.com/redis/go-redis/v9"
)

var buyerID = uuid.MustParse("11111111-1111-1111-1111-111111111111")

func database(t *testing.T, prepare bool) *postgres.DB {
	t.Helper()
	raw := os.Getenv("PERSO_AUDIT_DATABASE_URL")
	if raw == "" {
		t.Skip("PERSO_AUDIT_DATABASE_URL unavailable")
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 55439 || cfg.ConnConfig.Database != "perso_audit" {
		t.Fatal("refusing non-audit PostgreSQL endpoint")
	}
	ctx := context.Background()
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	schema := "audit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	db := &postgres.DB{Pool: pool}
	t.Cleanup(func() {
		pool.Close()
		_, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		if err != nil {
			t.Error(err)
		}
		admin.Close()
	})

	if prepare {
		if err = db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO users(id,username) VALUES($1,'audit_buyer')`, buyerID); err != nil {
			t.Fatal(err)
		}
	}

	return db
}

func product(t *testing.T, db *postgres.DB, name string) *domain.Product {
	t.Helper()
	p := &domain.Product{ID: uuid.New(), Name: name, NormalizedName: strings.ToLower(name), Brand: "Apple", Model: name, Category: "Smartphone"}
	if err := postgres.NewProductRepo(db).Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}
func offer(t *testing.T, db *postgres.DB, p *domain.Product, source string, amount int64, currency domain.Currency) *domain.Offer {
	t.Helper()
	o := &domain.Offer{ProductID: &p.ID, SourceMessageID: source, SellerID: 123, Brand: "Apple", Model: p.Model, Condition: domain.ConditionNew, Quantity: 1, PriceAmount: &amount, PriceCurrency: currency, PriceConfidence: 0.9, ExtractionConfidence: 0.9, SellerCity: "تهران"}
	if err := postgres.NewOfferRepo(db).Create(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestFreshDatabaseMigrations(t *testing.T) {
	db := database(t, false)
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("clean migration failed: %v", err)
	}
}

func TestMultiProductMessageRetainsBothOffers(t *testing.T) {
	db := database(t, true)
	a := product(t, db, "iPhone 15 Pro")
	b := product(t, db, "Samsung S24 Ultra")
	offer(t, db, a, "-1001_8", 98_000_000, domain.CurrencyToman)
	offer(t, db, b, "-1001_8", 80_000_000, domain.CurrencyToman)
	var n int
	if err := db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM offers").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("two products sharing a Telegram message produced %d offer(s), want 2", n)
	}
}

func TestReplayedOfferDoesNotDuplicatePriceHistory(t *testing.T) {
	db := database(t, true)
	p := product(t, db, "iPhone")
	offer(t, db, p, "-1001_9", 100, domain.CurrencyToman)
	offer(t, db, p, "-1001_9", 100, domain.CurrencyToman)
	var n int
	db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM price_observations").Scan(&n)
	if n != 1 {
		t.Fatalf("replayed identical offer created %d historical observations, want 1", n)
	}
}

func TestComparisonNormalizesCurrency(t *testing.T) {
	db := database(t, true)
	p := product(t, db, "iPhone")
	offer(t, db, p, "-1001_10", 100, domain.CurrencyToman)
	offer(t, db, p, "-1001_11", 1000, domain.CurrencyRial)
	rows, err := postgres.NewAnalyticsRepo(db).GetConditionComparison(context.Background(), &p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].AvgPrice != 100 {
		t.Fatalf("equivalent 100 Toman and 1000 Rial prices must average 100 Toman: %+v", rows)
	}
}

func TestStatisticsKnownPrices(t *testing.T) {
	db := database(t, true)
	p := product(t, db, "iPhone")
	for i, v := range []int64{100, 200, 300} {
		offer(t, db, p, fmt.Sprintf("-1001_%d", i), v, domain.CurrencyToman)
	}
	e := analytics.NewEngine(postgres.NewOfferRepo(db), postgres.NewProductRepo(db), postgres.NewAnalyticsRepo(db))
	stats, err := e.CalculateProductStatistics(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.MinPrice != 100 || stats.AvgPrice != 200 || stats.MedianPrice != 200 || stats.MaxPrice != 300 || stats.SampleCount != 3 {
		t.Fatalf("incorrect known statistics: %+v", stats)
	}
	byCity, err := postgres.NewAnalyticsRepo(db).GetCityComparison(context.Background(), &p.ID)
	if err != nil || len(byCity) != 1 || byCity[0].AvgPrice != 200 || byCity[0].MinPrice != 100 || byCity[0].MaxPrice != 300 {
		t.Fatalf("incorrect city comparison: %v %+v", err, byCity)
	}
	byBrand, err := postgres.NewAnalyticsRepo(db).GetBrandComparison(context.Background())
	if err != nil || len(byBrand) != 1 || byBrand[0].AvgPrice != 200 {
		t.Fatalf("incorrect brand comparison: %v %+v", err, byBrand)
	}
	lowest, _, err := postgres.NewOfferRepo(db).List(context.Background(), postgres.OfferFilter{ProductID: &p.ID, SortBy: domain.SortByLowestPrice, Limit: 1})
	if err != nil || len(lowest) != 1 || *lowest[0].PriceAmount != 100 {
		t.Fatalf("incorrect lowest price: %v %+v", err, lowest)
	}
	history, err := postgres.NewAnalyticsRepo(db).GetHistoricalPrices(context.Background(), p.ID, time.Now().Add(-time.Hour))
	if err != nil || len(history) != 3 {
		t.Fatalf("incorrect historical observations: %v %+v", err, history)
	}
}

func TestExpiredOfferIsExcludedFromStatistics(t *testing.T) {
	db := database(t, true)
	p := product(t, db, "iPhone")
	offer(t, db, p, "current", 100, domain.CurrencyToman)
	expired := offer(t, db, p, "expired", 50, domain.CurrencyToman)
	past := time.Now().Add(-time.Hour)
	expired.ExpirationDate = &past
	if _, err := db.Pool.Exec(context.Background(), "UPDATE offers SET expiration_date=$1 WHERE id=$2", past, expired.ID); err != nil {
		t.Fatal(err)
	}
	e := analytics.NewEngine(postgres.NewOfferRepo(db), postgres.NewProductRepo(db), postgres.NewAnalyticsRepo(db))
	stats, err := e.CalculateProductStatistics(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.MinPrice != 100 || stats.SampleCount != 1 {
		t.Fatalf("expired offer polluted buying statistics: %+v", stats)
	}
}

func TestBestOfferRankingBeforePagination(t *testing.T) {
	db := database(t, true)
	p := product(t, db, "iPhone")
	best := offer(t, db, p, "-1001_best", 50, domain.CurrencyToman)
	offer(t, db, p, "-1001_newest", 150, domain.CurrencyToman)
	e := analytics.NewEngine(postgres.NewOfferRepo(db), postgres.NewProductRepo(db), postgres.NewAnalyticsRepo(db))
	h := handlers.NewOfferHandler(postgres.NewOfferRepo(db), nil, e)
	w := httptest.NewRecorder()
	h.List(w, httptest.NewRequest("GET", "/offers?sort_by=best_offer&limit=1&product_id="+p.ID.String(), nil))
	var resp struct {
		Data []domain.ScoredOffer `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Offer.ID != best.ID {
		t.Fatalf("globally best offer excluded by pre-ranking pagination: %s", w.Body.String())
	}
}

func TestSourceStatusMutationChecksOwner(t *testing.T) {
	db := database(t, true)
	s := &domain.TelegramSource{OwnerID: buyerID, TelegramPeerID: -10055, Title: "fixture", SourceType: domain.TelegramSourceTypeForum, DetectedType: domain.TelegramSourceTypeForum, AccessType: domain.AccessTypePublic, IsActive: true, IngestionStatus: domain.IngestionStatusActive}
	repo := postgres.NewTelegramSourceRepo(db)
	if err := repo.Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	h := handlers.NewTelegramSourceHandler(repo, nil)
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("id", s.ID.String())
	req := httptest.NewRequest("PUT", "/", strings.NewReader(`{"ingestion_status":"paused"}`))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
	req = req.WithContext(context.WithValue(req.Context(), httputil.UserContextKey, uuid.New()))
	w := httptest.NewRecorder()
	h.UpdateStatus(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-owner status mutation allowed: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDuplicateConversationInitialization(t *testing.T) {
	db := database(t, true)
	p := product(t, db, "iPhone")
	o := offer(t, db, p, "-1001_conversation", 100, domain.CurrencyToman)
	m := conversation.NewManager(&config.Config{ConversationMaxQuestions: 7}, postgres.NewConversationRepo(db), nil, postgres.NewAuditRepo(db), nil, llm.NewMockProvider())
	for i := 0; i < 2; i++ {
		if _, err := m.InitConversation(context.Background(), o, []domain.MissingField{domain.FieldCity}); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM conversations WHERE offer_id=$1", o.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("same offer initialized %d conversation(s), want 1", n)
	}
}

func redisFixture(t *testing.T) *queue.Client {
	t.Helper()
	addr := os.Getenv("PERSO_AUDIT_REDIS_ADDR")
	if addr == "" {
		t.Skip("PERSO_AUDIT_REDIS_ADDR unavailable")
	}
	if addr != "127.0.0.1:56389" {
		t.Fatal("refusing non-audit Redis endpoint")
	}
	c, err := queue.Connect(context.Background(), addr, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func TestConsumerGroupDoesNotSkipExistingBacklog(t *testing.T) {
	c := redisFixture(t)
	ctx := context.Background()
	key := "audit:" + uuid.NewString()
	t.Cleanup(func() { c.Rdb.Del(ctx, key) })
	if _, err := c.Rdb.XAdd(ctx, &redis.XAddArgs{Stream: key, Values: map[string]interface{}{"payload": "{}"}}).Result(); err != nil {
		t.Fatal(err)
	}
	if err := c.EnsureConsumerGroup(ctx, key, "audit"); err != nil {
		t.Fatal(err)
	}
	entries, err := c.Rdb.XReadGroup(ctx, &redis.XReadGroupArgs{Group: "audit", Consumer: "one", Streams: []string{key, ">"}, Count: 1, Block: -1}).Result()
	if err != nil || len(entries) == 0 {
		t.Fatalf("existing backlog skipped by new consumer group: %v", err)
	}
}
func TestFailedStreamWorkIsRetried(t *testing.T) {
	c := redisFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	key := "audit:" + uuid.NewString()
	t.Cleanup(func() { c.Rdb.Del(context.Background(), key) })
	if err := c.EnsureConsumerGroup(ctx, key, "audit"); err != nil {
		t.Fatal(err)
	}
	p := queue.NewProducer(c, key)
	if _, err := p.PublishMessage(ctx, &domain.TelegramMessage{TelegramChatID: -1, TelegramMessageID: 1}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	consumer := queue.NewConsumer(c, key, "audit", "fixture", 1)
	if err := consumer.Start(ctx, func(context.Context, string, *queue.StreamPayload) error {
		calls++
		if calls == 1 {
			return fmt.Errorf("transient fixture failure")
		}
		cancel()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls < 2 {
		t.Fatalf("transient failure invoked handler %d time(s); pending work was never recovered", calls)
	}
}

func TestMongoEditsUpdateCurrentTextWithoutDuplicates(t *testing.T) {
	uri := os.Getenv("PERSO_AUDIT_MONGO_URI")
	if uri == "" {
		t.Skip("PERSO_AUDIT_MONGO_URI unavailable")
	}
	if uri != "mongodb://127.0.0.1:57039" {
		t.Fatal("refusing non-audit MongoDB endpoint")
	}
	ctx := context.Background()
	db, err := mongodb.Connect(ctx, uri, "audit_"+strings.ReplaceAll(uuid.NewString(), "-", ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Database.Drop(context.Background()); db.Close(context.Background()) })
	r := mongodb.NewMessagesRepo(db)
	m := &domain.TelegramMessage{TelegramChatID: -1, TelegramMessageID: 1, MessageText: "original price 100", MessageDate: time.Now()}
	if err = r.UpsertMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	m.MessageText = "edited price 200"
	m.EditedDate = &now
	if err = r.UpsertMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetByChatAndMessageID(ctx, -1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.OriginalMessageText != "original price 100" {
		t.Fatalf("original text not preserved: %q", got.OriginalMessageText)
	}
	if got.MessageText != m.MessageText {
		t.Fatalf("worker rereads stale text after edit: got=%q want=%q", got.MessageText, m.MessageText)
	}
}

func TestMedicalProductFrontendCannotEnterDeterministicBackend(t *testing.T) {
	e := extraction.NewExtractor().ExtractFromMessage(&domain.TelegramMessage{MessageText: "فروش دستگاه فشارسنج امرن M3 اصل قیمت ۳ میلیون تومان تهران"})
	if len(e.ExtractedProducts) == 0 {
		t.Fatal("frontend medical product has no backend deterministic extraction support")
	}
}

func mongoFixture(t *testing.T) *mongodb.DB {
	t.Helper()
	uri := os.Getenv("PERSO_AUDIT_MONGO_URI")
	if uri == "" {
		t.Skip("PERSO_AUDIT_MONGO_URI unavailable")
	}
	if uri != "mongodb://127.0.0.1:57039" {
		t.Fatal("refusing non-audit MongoDB endpoint")
	}
	db, err := mongodb.Connect(context.Background(), uri, "audit_"+strings.ReplaceAll(uuid.NewString(), "-", ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Database.Drop(context.Background()); db.Close(context.Background()) })
	return db
}

// Real persistence and worker processing, synthetic Telegram entities, LLM disabled.
// This is a backend fixture pipeline, not a live Telegram/LLM or React end-to-end test.
func TestBackendFixturePipelineBySourceType(t *testing.T) {
	for _, sourceType := range []domain.TelegramSourceType{domain.TelegramSourceTypeBasicGroup, domain.TelegramSourceTypeSupergroup, domain.TelegramSourceTypeForum, domain.TelegramSourceTypeBroadcastChannel} {
		t.Run(string(sourceType), func(t *testing.T) {
			ctx := context.Background()
			db := database(t, true)
			mongoDB := mongoFixture(t)
			rc := redisFixture(t)
			p := product(t, db, "iPhone 15 Pro")
			interest := &domain.Interest{UserID: buyerID, ProductID: p.ID, BuyEnabled: true}
			if err := postgres.NewInterestRepo(db).Create(ctx, interest); err != nil {
				t.Fatal(err)
			}
			source := &domain.TelegramSource{ID: uuid.New(), OwnerID: buyerID, TelegramPeerID: -100123, Title: "synthetic source fixture", SourceType: sourceType, DetectedType: sourceType, AccessType: domain.AccessTypePublic, IsActive: true, IngestionStatus: domain.IngestionStatusActive}
			if err := postgres.NewTelegramSourceRepo(db).Create(ctx, source); err != nil {
				t.Fatal(err)
			}
			raw := &tg.Message{ID: 7, PeerID: &tg.PeerChannel{ChannelID: 123}, FromID: &tg.PeerUser{UserID: 77}, Date: int(time.Now().Unix()), Message: "فروش آیفون ۱۵ پرو ۲۵۶ گیگ قیمت ۹۸ میلیون تومان تهران", ReplyTo: &tg.MessageReplyHeader{ForumTopic: true, ReplyToTopID: 90, ReplyToMsgID: 91}}
			msg, err := telegram.GetIngestionAdapter(sourceType, false).AdaptMessage(ctx, tg.Entities{}, raw, source)
			if err != nil {
				t.Fatal(err)
			}
			messages := mongodb.NewMessagesRepo(mongoDB)
			if err := messages.UpsertMessage(ctx, msg); err != nil {
				t.Fatal(err)
			}
			key := "audit:" + uuid.NewString()
			t.Cleanup(func() { rc.Rdb.Del(context.Background(), key) })
			if err := rc.EnsureConsumerGroup(ctx, key, "fixture"); err != nil {
				t.Fatal(err)
			}
			if _, err := queue.NewProducer(rc, key).PublishMessage(ctx, msg); err != nil {
				t.Fatal(err)
			}
			proc := worker.NewMessageProcessor(&config.Config{}, messages, mongodb.NewExtractionRepo(mongoDB), nil, nil, matching.NewMatcher(postgres.NewProductRepo(db)), postgres.NewOfferRepo(db), postgres.NewProductRepo(db), postgres.NewInterestRepo(db), postgres.NewSellerProfileRepo(db), postgres.NewMatchResultRepo(db), nil, nil, nil)
			readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			calls := 0

			handled := make(chan error, 1)
			finished := make(chan error, 1)
			go func() {
				finished <- queue.NewConsumer(rc, key, "fixture", "audit", 1).Start(readCtx, func(c context.Context, id string, payload *queue.StreamPayload) error {
					calls++
					err := proc.ProcessStreamMessage(c, id, payload)
					handled <- err
					return err
				})
			}()
			select {
			case err := <-handled:
				if err != nil {
					cancel()
					<-finished
					t.Fatal(err)
				}
			case <-readCtx.Done():
				cancel()
				<-finished
				t.Fatal("worker did not process fixture")
			}
			for {
				pending, err := rc.Rdb.XPending(ctx, key, "fixture").Result()
				if err != nil {
					cancel()
					<-finished
					t.Fatal(err)
				}
				if pending.Count == 0 {
					break
				}
				select {
				case <-readCtx.Done():
					cancel()
					<-finished
					t.Fatal("worker did not acknowledge fixture")
				case <-time.After(10 * time.Millisecond):
				}
			}
			cancel()
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("worker invocation count=%d", calls)
			}

			offers, total, err := postgres.NewOfferRepo(db).List(ctx, postgres.OfferFilter{ProductID: &p.ID})
			if err != nil {
				t.Fatal(err)
			}
			if total != 1 || offers[0].PriceAmount == nil || *offers[0].PriceAmount != 98_000_000 {
				t.Fatalf("wrong persisted offer: %+v", offers)
			}
			matches, err := postgres.NewMatchResultRepo(db).ListByUserID(ctx, buyerID, 10, 0)
			if err != nil || len(matches) != 1 {
				t.Fatalf("buyer interest not matched: %v %+v", err, matches)
			}
			stored, err := messages.GetByChatAndMessageID(ctx, msg.TelegramChatID, 7)
			if err != nil {
				t.Fatal(err)
			}
			if sourceType == domain.TelegramSourceTypeForum && (stored.TopicID == nil || *stored.TopicID != 90) {
				t.Fatal("topic lost across Mongo/worker boundary")
			}
			if sourceType == domain.TelegramSourceTypeBroadcastChannel && offers[0].SellerID != 0 {
				t.Fatal("channel post assigned a fabricated personal seller")
			}
		})
	}
}

func TestPendingWorkRecoveredAfterConsumerRestart(t *testing.T) {
	c := redisFixture(t)
	ctx := context.Background()
	key := "audit:" + uuid.NewString()
	t.Cleanup(func() { c.Rdb.Del(ctx, key) })
	if err := c.EnsureConsumerGroup(ctx, key, "fixture"); err != nil {
		t.Fatal(err)
	}
	queue.NewProducer(c, key).PublishMessage(ctx, &domain.TelegramMessage{TelegramChatID: -1, TelegramMessageID: 1})
	_, err := c.Rdb.XReadGroup(ctx, &redis.XReadGroupArgs{Group: "fixture", Consumer: "dead-worker", Streams: []string{key, ">"}, Count: 1, Block: -1}).Result()
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	calls := 0
	err = queue.NewConsumer(c, key, "fixture", "replacement", 1).Start(runCtx, func(context.Context, string, *queue.StreamPayload) error { calls++; cancel(); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("replacement worker never reclaimed pending work; calls=%d", calls)
	}
}

func TestConcurrentFreshDatabaseMigrations(t *testing.T) {
	db := database(t, false)
	errs := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() { errs <- db.Migrate(context.Background()) }()
	}
	for i := 0; i < 3; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent migration: %v", err)
		}
	}
}
