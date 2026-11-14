//go:build audit

package telegram

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gotd/td/tg"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/perso/perso-mind/internal/config"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/queue"
	"github.com/perso/perso-mind/internal/storage/mongodb"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

func TestNormalizedSourceIngestionAndPause(t *testing.T) {
	dsn := os.Getenv("PERSO_AUDIT_DATABASE_URL")
	uri := os.Getenv("PERSO_AUDIT_MONGO_URI")
	addr := os.Getenv("PERSO_AUDIT_REDIS_ADDR")
	if dsn == "" || uri == "" || addr == "" {
		t.Skip("disposable audit infrastructure is not configured")
	}
	if dsn != "postgres://postgres@127.0.0.1:55439/perso_audit?sslmode=disable" || uri != "mongodb://127.0.0.1:57039" || addr != "127.0.0.1:56389" {
		t.Fatal("refusing non-audit infrastructure")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "audit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	pc, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pc.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	db := &postgres.DB{Pool: pool}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner := uuid.New()
	if _, err = pool.Exec(ctx, "INSERT INTO users(id,username)VALUES($1,$2)", owner, "audit"); err != nil {
		t.Fatal(err)
	}
	mongoDB, err := mongodb.Connect(ctx, uri, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer mongoDB.Close(ctx)
	defer mongoDB.Database.Drop(ctx)
	rc, err := queue.Connect(ctx, addr, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	key := "audit:" + uuid.NewString()
	defer rc.Rdb.Del(ctx, key)
	sr := postgres.NewTelegramSourceRepo(db)
	wr := postgres.NewWatchSourceRepo(db)
	mr := mongodb.NewMessagesRepo(mongoDB)
	source := &domain.TelegramSource{OwnerID: owner, TelegramPeerID: -1000000000123, Title: "fixture", SourceType: domain.TelegramSourceTypeForum, DetectedType: domain.TelegramSourceTypeForum, AccessType: domain.AccessTypePublic, IsActive: true, IngestionStatus: domain.IngestionStatusActive}
	if err = sr.Create(ctx, source); err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(&config.Config{TelegramAppID: 1, TelegramSessionDir: t.TempDir(), TelegramSessionStorageKey: "audit-only-fixture-key"}, wr, sr, mr, nil, queue.NewProducer(rc, key))
	if err != nil {
		t.Fatal(err)
	}
	msg := &tg.Message{ID: 8, PeerID: &tg.PeerChannel{ChannelID: 123}, FromID: &tg.PeerUser{UserID: 77}, Date: int(time.Now().Unix()), Message: "fixture", ReplyTo: &tg.MessageReplyHeader{ForumTopic: true, ReplyToTopID: 6, ReplyToMsgID: 7}}
	if err = c.handleTelegramMessageClass(ctx, tg.Entities{}, msg, false); err != nil {
		t.Fatal(err)
	}
	got, err := mr.GetByChatAndMessageID(ctx, source.TelegramPeerID, 8)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.SourceID != source.ID.String() || got.TopicID == nil || *got.TopicID != 6 {
		t.Fatalf("normalized-only source was discarded or lost topic context: %+v", got)
	}
	checkpoint, err := sr.GetByID(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.LastMessageID != 8 {
		t.Fatal("checkpoint not advanced after persistence")
	}
	legacy := &domain.WatchSource{UserID: owner, TelegramChatID: source.TelegramPeerID, Title: "legacy fixture", SourceType: "channel", BuyEnabled: true, MonitoringStatus: domain.MonitoringStatusActive}
	if err = wr.Create(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if err = sr.UpdateStatus(ctx, source.ID, domain.IngestionStatusPaused, nil); err != nil {
		t.Fatal(err)
	}
	msg.ID = 9
	if err = c.handleTelegramMessageClass(ctx, tg.Entities{}, msg, false); err != nil {
		t.Fatal(err)
	}
	got, err = mr.GetByChatAndMessageID(ctx, source.TelegramPeerID, 9)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("legacy registry bypassed normalized source pause")
	}
}
