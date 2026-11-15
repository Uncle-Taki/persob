package postgres_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

func testDSN() string {
	if d := os.Getenv("PERSO_TEST_PG_DSN"); d != "" {
		return d
	}
	return "postgres://postgres@127.0.0.1:55439/perso_audit?sslmode=disable"
}

// covDB opens a disposable schema-isolated connection to the local audit
// PostgreSQL and runs migrations inside it. It refuses non-local endpoints and
// skips (rather than fails) when the disposable database is unreachable.
func covDB(t *testing.T, migrate bool) *postgres.DB {
	t.Helper()
	raw := testDSN()
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" {
		t.Fatalf("refusing non-local PostgreSQL endpoint %q", cfg.ConnConfig.Host)
	}
	ctx := context.Background()
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create admin pool: %v", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	err = admin.Ping(pingCtx)
	cancel()
	if err != nil {
		admin.Close()
		t.Skipf("disposable PostgreSQL unavailable, skipping: %v", err)
	}

	schema := "cov_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatalf("create schema: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		admin.Close()
		t.Fatalf("create pool: %v", err)
	}
	db := &postgres.DB{Pool: pool}
	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		admin.Close()
	})

	if migrate {
		if err := db.Migrate(ctx); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	return db
}

var (
	covBuyerID  = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	covOwnerID  = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	covProductA = uuid.MustParse("10000000-0000-0000-0000-000000000001")
	covProductB = uuid.MustParse("10000000-0000-0000-0000-000000000002")
	covOfferID1 = uuid.MustParse("20000000-0000-0000-0000-000000000001")
	covOfferID2 = uuid.MustParse("20000000-0000-0000-0000-000000000002")
	covInterest = uuid.MustParse("30000000-0000-0000-0000-000000000001")
	covNow      = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
)

// covUser inserts a user row directly and returns its ID.
func covUser(t *testing.T, db *postgres.DB, id uuid.UUID, username string) uuid.UUID {
	t.Helper()
	if _, err := db.Pool.Exec(context.Background(), `INSERT INTO users(id, username) VALUES($1, $2) ON CONFLICT (id) DO NOTHING`, id, username); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func covProduct(t *testing.T, db *postgres.DB, id uuid.UUID, name, brand, model, category string) *domain.Product {
	t.Helper()
	p := &domain.Product{
		ID:             id,
		Name:           name,
		NormalizedName: strings.ToLower(name),
		Brand:          brand,
		Manufacturer:   "Maker " + name,
		Model:          model,
		Category:       category,
	}
	if err := postgres.NewProductRepo(db).Create(context.Background(), p); err != nil {
		t.Fatalf("create product: %v", err)
	}
	return p
}

func covOffer(t *testing.T, db *postgres.DB, id, productID uuid.UUID, source string, sellerID int64, amount int64, currency domain.Currency) *domain.Offer {
	t.Helper()
	prod := &productID
	o := &domain.Offer{
		ID:                   id,
		ProductID:            prod,
		SourceMessageID:      source,
		SellerID:             sellerID,
		Brand:                "Apple",
		Manufacturer:         "Apple Inc",
		Model:                "iPhone 15",
		Condition:            domain.ConditionNew,
		Quantity:             1,
		PriceAmount:          &amount,
		PriceCurrency:        currency,
		PriceConfidence:      0.9,
		ExtractionConfidence: 0.9,
		SellerCity:           "Tehran",
		Branded:              true,
		BrandedKnown:         true,
	}
	if err := postgres.NewOfferRepo(db).Create(context.Background(), o); err != nil {
		t.Fatalf("create offer: %v", err)
	}
	return o
}
