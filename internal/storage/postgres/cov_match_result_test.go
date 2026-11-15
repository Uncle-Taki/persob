package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

func TestCovMatchResultCreateAndListByOfferID(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewMatchResultRepo(db)
	covUser(t, db, covBuyerID, "buyer1")
	covProduct(t, db, covProductA, "P", "Apple", "M", "C")
	covOfferForCreate(t, db, covOfferID1, covProductA, "mr-1", 100)

	matched := map[string]interface{}{"brand": "apple"}
	unmatched := map[string]interface{}{"condition": "new"}
	if err := repo.Create(ctx, &domain.OfferMatchResult{
		OfferID: covOfferID1, UserID: covBuyerID, ProductID: covProductA,
		MatchScore: 0.95, MatchExplanation: "same brand",
		MatchedAttributes: matched, UnmatchedAttributes: unmatched,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.ListByOfferID(ctx, covOfferID1)
	if err != nil {
		t.Fatalf("list by offer: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	r := got[0]
	if r.OfferID != covOfferID1 || r.UserID != covBuyerID || r.MatchScore != 0.95 || r.MatchExplanation != "same brand" {
		t.Fatalf("fields mismatch: %+v", r)
	}
	if r.MatchedAttributes["brand"] != "apple" || r.UnmatchedAttributes["condition"] != "new" {
		t.Fatalf("attributes mismatch: %+v / %+v", r.MatchedAttributes, r.UnmatchedAttributes)
	}
	if r.CreatedAt.IsZero() || r.ID == uuid.Nil {
		t.Fatalf("identity missing: %+v", r)
	}

	empty, err := repo.ListByOfferID(ctx, uuid.New())
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected none: %v / %v", empty, err)
	}
}

func TestCovMatchResultCreateErrors(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewMatchResultRepo(db)

	// FK violation: unknown offer.
	err := repo.Create(ctx, &domain.OfferMatchResult{OfferID: uuid.New(), UserID: uuid.New(), ProductID: covProductA, MatchScore: 1})
	if err == nil || !strings.Contains(err.Error(), "create offer match result") {
		t.Fatalf("expected FK error, got %v", err)
	}

	covUser(t, db, covBuyerID, "buyer1")
	covProduct(t, db, covProductA, "P", "Apple", "M", "C")
	covOfferForCreate(t, db, covOfferID1, covProductA, "mr-err", 100)
	// First valid row for the (offer, user) pair succeeds.
	if err := repo.Create(ctx, &domain.OfferMatchResult{OfferID: covOfferID1, UserID: covBuyerID, ProductID: covProductA, MatchScore: 0.9}); err != nil {
		t.Fatalf("first valid match result: %v", err)
	}
	// Duplicate (offer_id, user_id) must violate the unique index.
	if err := repo.Create(ctx, &domain.OfferMatchResult{OfferID: covOfferID1, UserID: covBuyerID, ProductID: covProductA, MatchScore: 0.5}); err == nil {
		t.Fatal("expected unique constraint violation")
	}
}

func TestCovMatchResultListByUserIDPaginationAndOrder(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewMatchResultRepo(db)
	covUser(t, db, covBuyerID, "buyer1")
	covUser(t, db, covOwnerID, "buyer2")
	covProduct(t, db, covProductA, "P", "Apple", "M", "C")
	covOfferForCreate(t, db, covOfferID1, covProductA, "mru-1", 100)
	covOfferForCreate(t, db, covOfferID2, covProductA, "mru-2", 200)

	mk := func(offerID uuid.UUID, score float64) {
		if err := repo.Create(ctx, &domain.OfferMatchResult{
			OfferID: offerID, UserID: covBuyerID, ProductID: covProductA,
			MatchScore: score, MatchedAttributes: map[string]interface{}{"s": score},
		}); err != nil {
			t.Fatalf("create row: %v", err)
		}
	}
	// covBuyerID has 2 distinct offers so far; add a third for the 3rd row.
	covOfferForCreate(t, db, uuid.New(), covProductA, "mru-3", 300)
	var third uuid.UUID
	if err := db.Pool.QueryRow(ctx, `SELECT id FROM offers WHERE source_message_id='mru-3'`).Scan(&third); err != nil {
		t.Fatal(err)
	}
	mk(covOfferID1, 0.3)
	mk(covOfferID2, 0.7)
	mk(third, 0.9)

	// Default limit (0 -> 50).
	all, err := repo.ListByUserID(ctx, covBuyerID, 0, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3, got %d", len(all))
	}
	// Ordered by created_at DESC: all created same second; last created first.
	if all[0].MatchScore != 0.9 || all[2].MatchScore != 0.3 {
		t.Fatalf("order mismatch: %v %v %v", all[0].MatchScore, all[1].MatchScore, all[2].MatchScore)
	}

	// Pagination: limit 2 offset 1.
	page, err := repo.ListByUserID(ctx, covBuyerID, 2, 1)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	if len(page) != 2 || page[0].MatchScore != 0.7 {
		t.Fatalf("pagination mismatch: %+v", page)
	}

	// Other user has none.
	none, err := repo.ListByUserID(ctx, covOwnerID, 0, 0)
	if err != nil || len(none) != 0 {
		t.Fatalf("expected none for buyer2: %v / %v", none, err)
	}
}

func TestCovMatchResultListQueryError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE offer_match_results CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewMatchResultRepo(db).ListByOfferID(context.Background(), covOfferID1); err == nil {
		t.Fatal("expected error")
	}
	if _, err := postgres.NewMatchResultRepo(db).ListByUserID(context.Background(), covBuyerID, 1, 0); err == nil {
		t.Fatal("expected error")
	}
}
