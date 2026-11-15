package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

func TestCovUserGetByID(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewUserRepo(db)

	// Full user with email + full name.
	fullName := "Full Name"
	if _, err := db.Pool.Exec(ctx, `INSERT INTO users(id,username,email,full_name,role) VALUES($1,'alice','a@b.c',$2,'admin')`, covBuyerID, fullName); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, covBuyerID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Username != "alice" || got.Email != "a@b.c" || got.FullName != "Full Name" || got.Role != domain.RoleAdmin {
		t.Fatalf("fields mismatch: %+v", got)
	}

	// Nullable email / full name left empty.
	if _, err := db.Pool.Exec(ctx, `INSERT INTO users(id,username) VALUES($1,'bob')`, covOwnerID); err != nil {
		t.Fatal(err)
	}
	got2, err := repo.GetByID(ctx, covOwnerID)
	if err != nil {
		t.Fatalf("get bob: %v", err)
	}
	if got2.Email != "" || got2.FullName != "" {
		t.Fatalf("expected empty nullables: %+v", got2)
	}

	missing, err := repo.GetByID(ctx, uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing user, got %+v", missing)
	}
}

func TestCovUserGetByIDError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE users CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewUserRepo(db).GetByID(context.Background(), covBuyerID); err == nil {
		t.Fatal("expected error after dropping users")
	}
}

func TestCovUserGetOrCreateDefaultUser(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewUserRepo(db)
	defaultID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	// First call creates it.
	first, err := repo.GetOrCreateDefaultUser(ctx)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if first.ID != defaultID || first.Username != "demo_buyer" || first.Email != "buyer@perso.local" || first.Role != domain.RoleBuyer {
		t.Fatalf("unexpected default user: %+v", first)
	}

	// Second call returns the existing row (no duplicate).
	second, err := repo.GetOrCreateDefaultUser(ctx)
	if err != nil {
		t.Fatalf("re-fetch: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected same default user id, got %v / %v", second.ID, first.ID)
	}
	var n int
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE id=$1", defaultID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("default user duplicated: %d", n)
	}
}

func TestCovUserGetOrCreateConflict(t *testing.T) {
	// The default id is absent, so GetByID returns nil and the create path
	// runs; if a row with the default id already existed (it should not here),
	// the existing-row path returns it. Either way a valid row comes back.
	db := covDB(t, true)
	repo := postgres.NewUserRepo(db)
	defaultID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	u, err := repo.GetOrCreateDefaultUser(context.Background())
	if err != nil || u == nil || u.ID != defaultID {
		t.Fatalf("get or create: %v / %+v", err, u)
	}
}

func TestCovUserGetOrCreateError(t *testing.T) {
	// Force the create path to fail: the default id is absent (so GetByID is
	// nil) but the default username 'demo_buyer' is already taken by another
	// row, so the INSERT's unique-username constraint is violated.
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), `INSERT INTO users(id,username) VALUES(gen_random_uuid(),'demo_buyer')`); err != nil {
		t.Fatal(err)
	}
	_, err := postgres.NewUserRepo(db).GetOrCreateDefaultUser(context.Background())
	if err == nil || !strings.Contains(err.Error(), "create default user") {
		t.Fatalf("expected create default user error, got %v", err)
	}
}
