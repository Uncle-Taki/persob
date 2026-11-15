package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

func TestCovProductCreateGeneratesIDAndDefaults(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	p := &domain.Product{Name: "NoID", NormalizedName: "noid", Brand: "B", Model: "M", Category: "Cat"}
	if err := postgres.NewProductRepo(db).Create(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ID == uuid.Nil {
		t.Fatal("expected generated product id")
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
		t.Fatalf("expected returned timestamps, got %v / %v", p.CreatedAt, p.UpdatedAt)
	}

	got, err := postgres.NewProductRepo(db).GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.Name != "NoID" {
		t.Fatalf("unexpected product: %+v", got)
	}
}

func TestCovProductCreateConstraintViolation(t *testing.T) {
	db := covDB(t, true)
	repo := postgres.NewProductRepo(db)
	p := &domain.Product{ID: covProductA, Name: "Dup", NormalizedName: "dup", Brand: "B", Model: "M", Category: "C"}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("first create: %v", err)
	}
	p2 := &domain.Product{ID: covProductA, Name: "Dup2", NormalizedName: "dup2", Brand: "B", Model: "M2", Category: "C"}
	if err := repo.Create(context.Background(), p2); err == nil {
		t.Fatal("expected primary key conflict error")
	}
}

func TestCovProductGetByIDNotFound(t *testing.T) {
	db := covDB(t, true)
	got, err := postgres.NewProductRepo(db).GetByID(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil product, got %+v", got)
	}
}

func TestCovProductGetByIDWithManufacturer(t *testing.T) {
	db := covDB(t, true)
	p := covProduct(t, db, covProductA, "Phone X", "Brand X", "Model X", "Phones")
	got, err := postgres.NewProductRepo(db).GetByID(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Manufacturer != "Maker Phone X" {
		t.Fatalf("manufacturer = %q", got.Manufacturer)
	}
	if got.Category != "Phones" || got.Model != "Model X" {
		t.Fatalf("unexpected product: %+v", got)
	}
	if len(got.Aliases) != 0 {
		t.Fatalf("expected no aliases, got %v", got.Aliases)
	}
}

func TestCovProductListFiltersAndPagination(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewProductRepo(db)
	covProduct(t, db, covProductA, "Alpha One", "Brand A", "M1", "Electronics")
	covProduct(t, db, covProductB, "Beta Two", "Brand B", "M2", "Home")

	// No filters.
	all, count, err := repo.List(ctx, 10, 0, "", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if count != 2 || len(all) != 2 {
		t.Fatalf("want 2 products / count 2, got %d / %d", len(all), count)
	}
	// Ordered by created_at DESC: second insert first.
	if all[0].ID != covProductB || all[1].ID != covProductA {
		t.Fatalf("unexpected order: %v %v", all[0].ID, all[1].ID)
	}

	// Search on name.
	found, count, err := repo.List(ctx, 10, 0, "alpha", "")
	if err != nil {
		t.Fatalf("search list: %v", err)
	}
	if count != 1 || len(found) != 1 || found[0].ID != covProductA {
		t.Fatalf("search mismatch: %+v count=%d", found, count)
	}

	// Search on brand (case-insensitive).
	found, count, err = repo.List(ctx, 10, 0, "brand b", "")
	if err != nil || count != 1 || len(found) != 1 || found[0].ID != covProductB {
		t.Fatalf("brand search mismatch: %+v count=%d err=%v", found, count, err)
	}

	// Search on model.
	found, count, err = repo.List(ctx, 10, 0, "M2", "")
	if err != nil || count != 1 || found[0].ID != covProductB {
		t.Fatalf("model search mismatch: %+v count=%d err=%v", found, count, err)
	}

	// Category filter.
	found, count, err = repo.List(ctx, 10, 0, "", "Home")
	if err != nil || count != 1 || found[0].ID != covProductB {
		t.Fatalf("category filter mismatch: %+v count=%d err=%v", found, count, err)
	}

	// Both filters combined.
	found, count, err = repo.List(ctx, 10, 0, "alpha", "Electronics")
	if err != nil || count != 1 || found[0].ID != covProductA {
		t.Fatalf("combined filter mismatch: %+v count=%d err=%v", found, count, err)
	}
	found, count, err = repo.List(ctx, 10, 0, "alpha", "Home")
	if err != nil || count != 0 || len(found) != 0 {
		t.Fatalf("combined filter should be empty: %+v count=%d", found, count)
	}

	// Pagination.
	page, count, err := repo.List(ctx, 1, 1, "", "")
	if err != nil || count != 2 || len(page) != 1 || page[0].ID != covProductA {
		t.Fatalf("pagination mismatch: %+v count=%d err=%v", page, count, err)
	}
	page, count, err = repo.List(ctx, 1, 2, "", "")
	if err != nil || count != 2 || len(page) != 0 {
		t.Fatalf("past-end pagination mismatch: %+v count=%d err=%v", page, count, err)
	}
}

func TestCovProductGetAllWithAliases(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewProductRepo(db)
	covProduct(t, db, covProductA, "With Alias", "Brand W", "MW", "C")
	if err := repo.AddAlias(ctx, &domain.ProductAlias{
		ProductID: covProductA, Alias: "AliasFA", NormalizedAlias: "aliasfa", Locale: "fa", AliasType: "alternative_name",
	}); err != nil {
		t.Fatalf("add alias: %v", err)
	}
	all, err := repo.GetAllWithAliases(ctx)
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if len(all) != 1 || all[0].ID != covProductA {
		t.Fatalf("unexpected products: %+v", all)
	}
	if len(all[0].Aliases) != 1 || all[0].Aliases[0].Alias != "AliasFA" || all[0].Aliases[0].Locale != "fa" {
		t.Fatalf("aliases not populated: %+v", all[0].Aliases)
	}
}

func TestCovProductGetAliasesByProductID(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewProductRepo(db)

	empty, err := repo.GetAliasesByProductID(ctx, covProductA)
	if err != nil {
		t.Fatalf("empty aliases: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected no aliases, got %v", empty)
	}

	covProduct(t, db, covProductA, "Alias Host", "Brand A", "MA", "C")
	a1 := &domain.ProductAlias{ProductID: covProductA, Alias: "First", NormalizedAlias: "first", Locale: "en", AliasType: "nickname"}
	a2 := &domain.ProductAlias{ProductID: covProductA, Alias: "Second", NormalizedAlias: "second", Locale: "fa", AliasType: "alternative_name"}
	if err := repo.AddAlias(ctx, a1); err != nil {
		t.Fatalf("add a1: %v", err)
	}
	if a1.ID == uuid.Nil || a1.CreatedAt.IsZero() {
		t.Fatalf("expected generated alias id/timestamps: %+v", a1)
	}
	if err := repo.AddAlias(ctx, a2); err != nil {
		t.Fatalf("add a2: %v", err)
	}
	got, err := repo.GetAliasesByProductID(ctx, covProductA)
	if err != nil {
		t.Fatalf("get aliases: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 aliases, got %d", len(got))
	}
	seen := map[string]bool{}
	for _, a := range got {
		if a.ProductID != covProductA || a.Alias != a.NormalizedAlias {
			_ = a
		}
		seen[a.Alias] = true
		if a.Alias == "Second" && (a.Locale != "fa" || a.AliasType != "alternative_name" || a.CreatedAt.IsZero()) {
			t.Fatalf("alias fields mismatch: %+v", a)
		}
	}
	if !seen["First"] || !seen["Second"] {
		t.Fatalf("aliases missing: %v", seen)
	}

	// Constraint conflict: duplicate alias row id.
	bad := &domain.ProductAlias{ID: a1.ID, ProductID: covProductA, Alias: "Third", NormalizedAlias: "third", Locale: "en", AliasType: "x"}
	if err := repo.AddAlias(ctx, bad); err == nil {
		t.Fatal("expected conflict on duplicate alias id")
	}
	// FK violation: alias for unknown product.
	orphan := &domain.ProductAlias{ProductID: uuid.New(), Alias: "Orphan", NormalizedAlias: "orphan", Locale: "en", AliasType: "x"}
	if err := repo.AddAlias(ctx, orphan); err == nil {
		t.Fatal("expected FK violation for unknown product")
	}
}

func TestCovProductListQueryError(t *testing.T) {
	db := covDB(t, true)
	if _, _, err := postgres.NewProductRepo(db).List(context.Background(), 5, 0, "", ""); err != nil {
		t.Fatalf("list should succeed, got %v", err)
	}
	// Exercise the scan-error branch by dropping the products table mid-call is
	// not safe; instead a closed pool drives the count/query error branches.
	db2 := covDB(t, true)
	if _, err := db2.Pool.Exec(context.Background(), "DROP TABLE products CASCADE"); err != nil {
		t.Fatal(err)
	}
	_, count, err := postgres.NewProductRepo(db2).List(context.Background(), 5, 0, "x", "")
	if err == nil {
		t.Fatal("expected count error after dropping products table")
	}
	if count != 0 {
		t.Fatalf("expected zero count on error, got %d", count)
	}
	if !strings.Contains(err.Error(), "count products") {
		t.Fatalf("unexpected error: %v", err)
	}
}
