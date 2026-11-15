package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

func TestCovSellerProfileUpsertInsertAndUpdate(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewSellerProfileRepo(db)
	until := covNow.Add(24 * time.Hour)

	// Insert.
	ins := &domain.SellerProfile{SellerID: 77, Username: "u77", DisplayName: "D77", City: "Tehran", TehranArea: "10", TotalOffers: 3, CooldownUntil: &until}
	if err := repo.Upsert(ctx, ins); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if ins.CreatedAt.IsZero() || ins.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not returned: %+v", ins)
	}

	// Update: empty strings must keep existing values; total_offers accumulates.
	// Note: the conflict path does not persist opted_out or cooldown_until.
	upd := &domain.SellerProfile{SellerID: 77, Username: "", DisplayName: "Renamed", City: "", TehranArea: "12", TotalOffers: 5, OptedOut: true}
	if err := repo.Upsert(ctx, upd); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := repo.GetByID(ctx, 77)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Username != "u77" {
		t.Fatalf("empty username should keep old value, got %q", got.Username)
	}
	if got.DisplayName != "Renamed" || got.City != "Tehran" || got.TehranArea != "12" {
		t.Fatalf("update fields mismatch: %+v", got)
	}
	if got.TotalOffers != 8 {
		t.Fatalf("total_offers should accumulate to 8, got %d", got.TotalOffers)
	}
	if got.OptedOut {
		t.Fatalf("upsert conflict must not persist opted_out: %+v", got)
	}
	if got.CooldownUntil == nil || !got.CooldownUntil.Equal(until) {
		t.Fatalf("cooldown mismatch: %+v", got.CooldownUntil)
	}
	// opted_out is managed through SetOptOut.
	if err := repo.SetOptOut(ctx, 77, true); err != nil {
		t.Fatalf("set opt out: %v", err)
	}
	got, err = repo.GetByID(ctx, 77)
	if err != nil || !got.OptedOut {
		t.Fatalf("opted_out not set: %+v / %v", got, err)
	}
}

func TestCovSellerProfileGetByIDNotFound(t *testing.T) {
	db := covDB(t, true)
	_, err := postgres.NewSellerProfileRepo(db).GetByID(context.Background(), 424242)
	if err == nil || err.Error() != "seller not found" {
		t.Fatalf("expected 'seller not found', got %v", err)
	}
}

func TestCovSellerProfileGetByIDError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE seller_profiles CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewSellerProfileRepo(db).GetByID(context.Background(), 1); err == nil {
		t.Fatal("expected error after dropping table")
	}
}

func TestCovSellerProfileUpsertError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE seller_profiles CASCADE"); err != nil {
		t.Fatal(err)
	}
	repo := postgres.NewSellerProfileRepo(db)
	err := repo.Upsert(context.Background(), &domain.SellerProfile{SellerID: 1})
	if err == nil || !strings.Contains(err.Error(), "upsert seller profile") {
		t.Fatalf("expected wrapped upsert error, got %v", err)
	}
	if err := repo.IncrementOfferCount(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "increment seller offer count") {
		t.Fatalf("expected wrapped increment error, got %v", err)
	}
	if err := repo.SetOptOut(context.Background(), 1, true); err == nil || !strings.Contains(err.Error(), "set seller opt out") {
		t.Fatalf("expected wrapped opt out error, got %v", err)
	}
	if err := repo.SetCooldown(context.Background(), 1, covNow); err == nil || !strings.Contains(err.Error(), "set seller cooldown") {
		t.Fatalf("expected wrapped cooldown error, got %v", err)
	}
}

func TestCovSellerProfileIncrementOfferCount(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewSellerProfileRepo(db)
	if err := repo.Upsert(ctx, &domain.SellerProfile{SellerID: 55, Username: "u55", TotalOffers: 2}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := repo.IncrementOfferCount(ctx, 55); err != nil {
			t.Fatalf("increment %d: %v", i, err)
		}
	}
	got, err := repo.GetByID(ctx, 55)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalOffers != 5 {
		t.Fatalf("expected 5 offers, got %d", got.TotalOffers)
	}

	// Increment on a missing seller: Exec succeeds, no rows changed, no error.
	if err := repo.IncrementOfferCount(ctx, 999999); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCovSellerProfileSetOptOutAndCooldown(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewSellerProfileRepo(db)
	if err := repo.Upsert(ctx, &domain.SellerProfile{SellerID: 66, Username: "u66"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetOptOut(ctx, 66, true); err != nil {
		t.Fatalf("opt out: %v", err)
	}
	until := covNow.Add(48 * time.Hour)
	if err := repo.SetCooldown(ctx, 66, until); err != nil {
		t.Fatalf("cooldown: %v", err)
	}
	got, err := repo.GetByID(ctx, 66)
	if err != nil {
		t.Fatal(err)
	}
	if !got.OptedOut || got.CooldownUntil == nil || !got.CooldownUntil.Equal(until) {
		t.Fatalf("state mismatch: %+v", got)
	}
	// Toggle opt-out off.
	if err := repo.SetOptOut(ctx, 66, false); err != nil {
		t.Fatalf("opt in: %v", err)
	}
	got, err = repo.GetByID(ctx, 66)
	if err != nil || got.OptedOut {
		t.Fatalf("opt-out not cleared: %+v / %v", got, err)
	}
}
