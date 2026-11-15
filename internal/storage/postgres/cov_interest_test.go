package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

func TestCovInterestCreateForcesPhase1Invariants(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewInterestRepo(db)
	covUser(t, db, covBuyerID, "buyer1")
	covProduct(t, db, covProductA, "P", "Apple", "M", "C")

	// Caller tries to enable sell: must be forced to false / buy true.
	ii := &domain.Interest{ID: covInterest, UserID: covBuyerID, ProductID: covProductA, SellEnabled: true, BuyEnabled: false}
	if err := repo.Create(ctx, ii); err != nil {
		t.Fatalf("create: %v", err)
	}
	if ii.SellEnabled || !ii.BuyEnabled {
		t.Fatalf("invariants not enforced: sell=%v buy=%v", ii.SellEnabled, ii.BuyEnabled)
	}
	if ii.ID != covInterest || ii.CreatedAt.IsZero() {
		t.Fatalf("unexpected identity: %+v", ii)
	}

	// Same (user, product) again: conflict path must reset buy_enabled.
	ii2 := &domain.Interest{UserID: covBuyerID, ProductID: covProductA, SellEnabled: true}
	if err := repo.Create(ctx, ii2); err != nil {
		t.Fatalf("re-create: %v", err)
	}
	if ii2.BuyEnabled != true {
		t.Fatalf("conflict path must keep buy_enabled=true, got %+v", ii2)
	}

	// FK violation for unknown user.
	if err := repo.Create(ctx, &domain.Interest{UserID: uuid.New(), ProductID: covProductA}); err == nil {
		t.Fatal("expected FK violation for unknown user")
	}
}

func TestCovInterestListByUserID(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewInterestRepo(db)
	covUser(t, db, covBuyerID, "buyer1")
	covUser(t, db, covOwnerID, "buyer2")
	pA := covProduct(t, db, covProductA, "Product A", "Apple", "MA", "Phones")
	pB := covProduct(t, db, covProductB, "Product B", "BrandB", "MB", "Home")

	if err := repo.Create(ctx, &domain.Interest{UserID: covBuyerID, ProductID: covProductA}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, &domain.Interest{UserID: covBuyerID, ProductID: covProductB}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, &domain.Interest{UserID: covOwnerID, ProductID: covProductA}); err != nil {
		t.Fatal(err)
	}

	list, err := repo.ListByUserID(ctx, covBuyerID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 interests, got %d", len(list))
	}
	seen := map[uuid.UUID]string{}
	for _, item := range list {
		if item.UserID != covBuyerID {
			t.Fatalf("interest belongs to another user: %+v", item)
		}
		if item.Product == nil {
			t.Fatalf("product join missing: %+v", item)
		}
		seen[item.ProductID] = item.Product.Name
	}
	if seen[covProductA] != "Product A" || seen[covProductB] != "Product B" {
		t.Fatalf("joined product names mismatch: %v", seen)
	}
	if pA == nil || pB == nil {
		t.Fatal("fixtures missing")
	}

	empty, err := repo.ListByUserID(ctx, uuid.New())
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected empty list for unknown user: %v / %v", empty, err)
	}
}

func TestCovInterestDelete(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewInterestRepo(db)
	covUser(t, db, covBuyerID, "buyer1")
	covProduct(t, db, covProductA, "P", "Apple", "M", "C")
	if err := repo.Create(ctx, &domain.Interest{ID: covInterest, UserID: covBuyerID, ProductID: covProductA}); err != nil {
		t.Fatal(err)
	}

	// Wrong user: unauthorized path.
	err := repo.Delete(ctx, covInterest, covOwnerID)
	if err == nil || !strings.Contains(err.Error(), "not found or unauthorized") {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
	// Correct user: row removed.
	if err := repo.Delete(ctx, covInterest, covBuyerID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var n int
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM interests").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("row not deleted, %d remain", n)
	}
}

func TestCovInterestFindInterestedUsers(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewInterestRepo(db)
	covUser(t, db, covBuyerID, "buyer1")
	covUser(t, db, covOwnerID, "buyer2")
	covProduct(t, db, covProductA, "P", "Apple", "M", "C")

	if err := repo.Create(ctx, &domain.Interest{UserID: covBuyerID, ProductID: covProductA}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, &domain.Interest{UserID: covOwnerID, ProductID: covProductA}); err != nil {
		t.Fatal(err)
	}

	users, err := repo.FindInterestedUsers(ctx, covProductA)
	if err != nil {
		t.Fatalf("find users: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("want 2 interested users, got %v", users)
	}
	byID := map[uuid.UUID]bool{}
	for _, u := range users {
		byID[u] = true
	}
	if !byID[covBuyerID] || !byID[covOwnerID] {
		t.Fatalf("unexpected users: %v", byID)
	}

	empty, err := repo.FindInterestedUsers(ctx, uuid.New())
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected no interested users: %v / %v", empty, err)
	}
}

func TestCovInterestFindInterestsByProductID(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewInterestRepo(db)
	covUser(t, db, covBuyerID, "buyer1")
	covProduct(t, db, covProductA, "P", "Apple", "M", "C")
	if err := repo.Create(ctx, &domain.Interest{ID: covInterest, UserID: covBuyerID, ProductID: covProductA}); err != nil {
		t.Fatal(err)
	}

	got, err := repo.FindInterestsByProductID(ctx, covProductA)
	if err != nil {
		t.Fatalf("find interests: %v", err)
	}
	if len(got) != 1 || got[0].ID != covInterest || got[0].UserID != covBuyerID || !got[0].BuyEnabled {
		t.Fatalf("unexpected interests: %+v", got)
	}

	empty, err := repo.FindInterestsByProductID(ctx, uuid.New())
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected none: %v / %v", empty, err)
	}
}

func TestCovInterestListQueryError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE interests CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewInterestRepo(db).ListByUserID(context.Background(), covBuyerID); err == nil {
		t.Fatal("expected list error")
	}
	if _, err := postgres.NewInterestRepo(db).FindInterestedUsers(context.Background(), covProductA); err == nil {
		t.Fatal("expected find users error")
	}
	if _, err := postgres.NewInterestRepo(db).FindInterestsByProductID(context.Background(), covProductA); err == nil {
		t.Fatal("expected find interests error")
	}
}
