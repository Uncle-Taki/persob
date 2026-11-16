//go:build audit

package audit_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/config"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/matching"
	"github.com/perso/perso-mind/internal/queue"
	"github.com/perso/perso-mind/internal/storage/mongodb"
	"github.com/perso/perso-mind/internal/storage/postgres"
	"github.com/perso/perso-mind/internal/worker"
	"go.mongodb.org/mongo-driver/bson"
)

// Multiple assigned buyers and repeated/edited deliveries share one listing and
// raw identity while preserving one match per eligible buyer.
func TestSharedIngestionReplayEditsAndMultipleBuyerInterests(t *testing.T) {
	ctx := context.Background()
	db := database(t, true)
	mongo := mongoFixture(t)
	p := product(t, db, "iPhone 15 Pro")
	otherAdmin, a, b := uuid.New(), uuid.New(), uuid.New()
	for i, u := range []uuid.UUID{buyerID, otherAdmin, a, b} {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO users(id,username,role,active) VALUES($1,$2,'buyer',true) ON CONFLICT(id) DO UPDATE SET active=true`, u, "shared_"+u.String()); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, `INSERT INTO interests(user_id,product_id) VALUES($1,$2)`, u, p.ID); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=ANY($1::uuid[])`, []uuid.UUID{buyerID, otherAdmin}); err != nil {
		t.Fatal(err)
	}
	sources := postgres.NewTelegramSourceRepo(db)
	source := &domain.TelegramSource{OwnerID: otherAdmin, TelegramPeerID: -100777, Title: "shared fixture", SourceType: domain.TelegramSourceTypeSupergroup, DetectedType: domain.TelegramSourceTypeSupergroup, IsActive: true, IngestionStatus: domain.IngestionStatusActive}
	if err := sources.Create(ctx, source); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO source_assignments(source_id,buyer_id) SELECT $1,unnest($2::uuid[])`, source.ID, []uuid.UUID{a, b}); err != nil {
		t.Fatal(err)
	}
	messages := mongodb.NewMessagesRepo(mongo)
	proc := worker.NewMessageProcessor(&config.Config{OperatorUserID: buyerID.String()}, messages, mongodb.NewExtractionRepo(mongo), nil, nil, matching.NewMatcher(postgres.NewProductRepo(db)), postgres.NewOfferRepo(db), postgres.NewProductRepo(db), postgres.NewInterestRepo(db), postgres.NewSellerProfileRepo(db), postgres.NewMatchResultRepo(db), nil, nil, nil)
	proc.SetSourceRepo(sources)
	msg := &domain.TelegramMessage{TelegramChatID: -100777, TelegramMessageID: 7, SenderID: 77, SourceID: source.ID.String(), MessageDate: time.Now().UTC().Truncate(time.Second), MessageText: "فروش آیفون ۱۵ پرو ۲۵۶ گیگ قیمت ۹۸ میلیون تومان تهران"}
	original := *msg
	run := func(m *domain.TelegramMessage) {
		t.Helper()
		if err := messages.UpsertMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
		payload := *m
		payload.SourceID = uuid.NewString()
		if err := proc.ProcessStreamMessage(ctx, "synthetic-replay", &queue.StreamPayload{Message: &payload}); err != nil {
			t.Fatal(err)
		}
	}
	run(msg)
	run(msg)
	var offers, observations, matches int
	counts := func(wantObservations int) {
		t.Helper()
		if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM offers WHERE source_id=$1 AND owner_id IS NULL`, source.ID).Scan(&offers); err != nil || offers != 1 {
			t.Fatalf("shared listing count=%d err=%v", offers, err)
		}
		if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM price_observations`).Scan(&observations); err != nil || observations != wantObservations {
			t.Fatalf("history count=%d want=%d err=%v", observations, wantObservations, err)
		}
		if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM offer_match_results`).Scan(&matches); err != nil || matches != 2 {
			t.Fatalf("replay duplicated or omitted assigned-buyer matches: %d err=%v", matches, err)
		}
		if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM offer_match_results WHERE user_id=ANY($1::uuid[])`, []uuid.UUID{a, b}).Scan(&matches); err != nil || matches != 2 {
			t.Fatalf("assigned buyers did not each receive one match: %d err=%v", matches, err)
		}
		if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM offer_match_results WHERE user_id=ANY($1::uuid[])`, []uuid.UUID{buyerID, otherAdmin}).Scan(&matches); err != nil || matches != 0 {
			t.Fatalf("unassigned administrators received buyer matches: %d err=%v", matches, err)
		}
	}
	counts(1)
	edited := msg.MessageDate.Add(time.Minute)
	msg.EditedDate = &edited
	msg.MessageText = "فروش آیفون ۱۵ پرو ۲۵۶ گیگ قیمت ۹۹ میلیون تومان تهران"
	run(msg)
	run(msg)
	run(&original)
	counts(2)
	var price int64
	if err := db.Pool.QueryRow(ctx, `SELECT price_amount FROM offers WHERE source_id=$1`, source.ID).Scan(&price); err != nil || price != 99_000_000 {
		t.Fatalf("old revision replaced latest price: %d %v", price, err)
	}
	raw, err := messages.GetByChatAndMessageID(ctx, msg.TelegramChatID, msg.TelegramMessageID)
	if err != nil || raw.MessageText != msg.MessageText || raw.OriginalMessageText != original.MessageText {
		t.Fatal("raw revision/original was lost", err)
	}
	n, err := mongo.Database.Collection("telegram_messages").CountDocuments(ctx, bson.M{})
	if err != nil || n != 1 {
		t.Fatal("raw message duplicated per buyer/replay", n, err)
	}
}
