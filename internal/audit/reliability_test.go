//go:build audit

package audit_test

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/config"
	"github.com/perso/perso-mind/internal/conversation"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/llm"
	"github.com/perso/perso-mind/internal/storage/postgres"
	"os"
	"testing"
	"time"
)

func TestNewerOfferRevisionWinsAndReplaysDoNotDuplicate(t *testing.T) {
	db := database(t, true)
	p := product(t, db, "iPhone 15 Pro")
	repo := postgres.NewOfferRepo(db)
	o := offer(t, db, p, "revision", 100, domain.CurrencyToman)
	now := time.Now().UTC()
	o.SourceRevisionAt = now
	amount := int64(200)
	o.PriceAmount = &amount
	if err := repo.Create(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	old := *o
	old.SourceRevisionAt = now.Add(-time.Hour)
	price := int64(50)
	old.PriceAmount = &price
	if err := repo.Create(context.Background(), &old); err != nil {
		t.Fatal(err)
	}
	if old.Applied {
		t.Fatal("stale revision applied")
	}
	if err := repo.Create(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetByID(context.Background(), o.ID)
	if err != nil || *stored.PriceAmount != 200 {
		t.Fatalf("newer revision lost: %v %+v", err, stored)
	}
	var count int
	db.Pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM price_observations WHERE offer_id=$1", o.ID).Scan(&count)
	if count != 2 {
		t.Fatalf("history count=%d want 2", count)
	}
}
func TestConversationConsentAndValidatedAnswersSurviveReload(t *testing.T) {
	ctx := context.Background()
	db := database(t, true)
	p := product(t, db, "iPhone")
	o := offer(t, db, p, "intake", 100, domain.CurrencyToman)
	o.OwnerID = &buyerID
	cfg := &config.Config{OutreachEnabled: true, ConversationMaxQuestions: 3, OperatorUserID: buyerID.String()}
	repo := postgres.NewConversationRepo(db)
	manager := conversation.NewManager(cfg, repo, nil, postgres.NewAuditRepo(db), nil, llm.NewMockProvider())
	c, err := manager.InitConversation(ctx, o, []domain.MissingField{domain.FieldPrice})
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Approve(ctx, c.ID, buyerID); err != nil {
		t.Fatal(err)
	}
	if err = manager.Approve(ctx, c.ID, buyerID); err == nil {
		t.Fatal("duplicate approval was accepted")
	}
	if _, err = manager.HandleIncomingSellerMessage(postgres.WithInbound(ctx, o.SellerID, 1, "بله"), c.ID, "بله"); err != nil {
		t.Fatal(err)
	}
	loaded, _ := repo.GetByID(ctx, c.ID)
	if !loaded.ConsentGranted || loaded.State != domain.ConvStateWaitingForReply {
		t.Fatalf("consent not durable: %+v", loaded)
	}
	if _, err = manager.HandleIncomingSellerMessage(postgres.WithInbound(ctx, o.SellerID, 2, "نمی‌دانم"), c.ID, "نمی‌دانم"); err != nil {
		t.Fatal(err)
	}
	loaded, _ = repo.GetByID(ctx, c.ID)
	if len(loaded.MissingFields) != 1 {
		t.Fatal("invalid answer removed a missing field")
	}
	if _, err = manager.HandleIncomingSellerMessage(postgres.WithInbound(ctx, o.SellerID, 3, "۲۰۰ تومان"), c.ID, "۲۰۰ تومان"); err != nil {
		t.Fatal(err)
	}
	loaded, _ = repo.GetByID(ctx, c.ID)
	stored, _ := postgres.NewOfferRepo(db).GetByID(ctx, o.ID)
	if loaded.State != domain.ConvStateCompleted || *stored.PriceAmount != 200 {
		t.Fatalf("intake did not update offer: %+v %+v", loaded, stored)
	}
	history, err := repo.QueueHistory(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) < 4 {
		t.Fatalf("inbound/outbound history missing: %+v", history)
	}
}
func TestStatisticsIncludeMoreThanFiveHundredEligibleListings(t *testing.T) {
	ctx := context.Background()
	db := database(t, true)
	p := product(t, db, "iPhone")
	_, err := db.Pool.Exec(ctx, `INSERT INTO offers(product_id,source_message_id,seller_id,price_amount) SELECT $1,'bulk_'||n,123,n FROM generate_series(1,600)n`, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	offers, err := postgres.NewOfferRepo(db).GetActiveOffersByProductID(ctx, p.ID)
	if err != nil || len(offers) != 600 {
		t.Fatalf("population truncated: %d %v", len(offers), err)
	}
}
func TestOfferConstraintsRejectInvalidValues(t *testing.T) {
	db := database(t, true)
	for i, sql := range []string{`INSERT INTO offers(source_message_id,seller_id,price_amount) VALUES('%d',1,-1)`, `INSERT INTO offers(source_message_id,seller_id,quantity) VALUES('%d',1,0)`} {
		if _, err := db.Pool.Exec(context.Background(), fmt.Sprintf(sql, i)); err == nil {
			t.Fatal("invalid offer accepted")
		}
	}
}

func TestUpgradeAdoptsExistingSchemaWithoutLosingOffers(t *testing.T) {
	ctx := context.Background()
	db := database(t, false)
	for _, file := range []string{"001_initial_schema.sql", "002_telegram_sources_and_marketplace.sql"} {
		sql, err := os.ReadFile("../storage/postgres/migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Pool.Exec(ctx, string(sql)); err != nil {
			t.Fatal(err)
		}
	}
	var id string
	if err := db.Pool.QueryRow(ctx, `INSERT INTO offers(source_message_id,seller_id,price_amount)VALUES('pre_upgrade',123,100) RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM offers WHERE id::text=$1 AND price_amount=100`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("upgrade lost an existing offer: %v count=%d", err, count)
	}
	db.Pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count)
	if count != 6 {
		t.Fatalf("migration ledger has %d entries, want 6", count)
	}
}
func TestTypedIntakeRejectsCrossOperatorApproval(t *testing.T) {
	ctx := context.Background()
	db := database(t, true)
	p := product(t, db, "iPhone")
	o := offer(t, db, p, "owner", 100, domain.CurrencyToman)
	o.OwnerID = &buyerID
	m := conversation.NewManager(&config.Config{OutreachEnabled: true}, postgres.NewConversationRepo(db), nil, nil, nil, nil)
	c, err := m.InitConversation(ctx, o, []domain.MissingField{domain.FieldCity})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Approve(ctx, c.ID, uuid.New()); err == nil {
		t.Fatal("another operator approved outreach")
	}
}
