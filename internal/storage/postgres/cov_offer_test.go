package postgres_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

func covOfferForCreate(t *testing.T, db *postgres.DB, id, productID uuid.UUID, source string, amount int64) *domain.Offer {
	t.Helper()
	prod := &productID
	o := &domain.Offer{
		ID:                id,
		ProductID:         prod,
		SourceMessageID:   source,
		SellerID:          9001,
		Brand:             "Apple",
		Model:             "iPhone 15",
		PriceAmount:       &amount,
		PriceCurrency:     domain.CurrencyToman,
		SourceRevisionAt:  covNow,
		SourcePublishedAt: &covNow,
	}
	if err := postgres.NewOfferRepo(db).Create(context.Background(), o); err != nil {
		t.Fatalf("create offer: %v", err)
	}
	return o
}

func TestCovOfferCreateDefaultsAndObservation(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewOfferRepo(db)
	covProduct(t, db, covProductA, "Phone", "Apple", "iPhone 15", "Phones")

	// Fully defaulted offer: no id, currency, status, condition, quantity,
	// revision, item key.
	prod := covProductA
	p100 := int64(100)
	o := &domain.Offer{ProductID: &prod, SourceMessageID: "msg-def", SellerID: 1, PriceAmount: &p100}
	if err := repo.Create(ctx, o); err != nil {
		t.Fatalf("create defaulted offer: %v", err)
	}
	if o.ID == uuid.Nil || !o.Applied {
		t.Fatalf("expected applied offer with generated id: %+v", o)
	}
	if o.PriceCurrency != domain.CurrencyToman || o.ListingStatus != domain.ListingStatusActive || o.Condition != domain.ConditionUnknown {
		t.Fatalf("defaults not applied: %+v", o)
	}
	if o.Quantity != 1 {
		t.Fatalf("quantity default = %d, want 1", o.Quantity)
	}
	if o.SourceItemKey != covProductA.String() {
		t.Fatalf("item key = %q, want product id", o.SourceItemKey)
	}
	if o.ContentHash == "" {
		t.Fatal("expected content hash")
	}
	// Epoch revision: observation recorded under the bare content hash.
	var obs int
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM price_observations").Scan(&obs); err != nil {
		t.Fatal(err)
	}
	if obs != 1 {
		t.Fatalf("expected 1 price observation, got %d", obs)
	}
	var revision string
	if err := db.Pool.QueryRow(ctx, "SELECT revision_hash FROM price_observations").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if revision == "" || strings.Contains(revision, ":") {
		t.Fatalf("epoch revision should be the bare content hash, got %q", revision)
	}

	// Identical replay must not duplicate history and keeps Applied=true.
	replay := &domain.Offer{ProductID: &prod, SourceMessageID: "msg-def", SellerID: 1, Quantity: 1, PriceAmount: priceOf(100)}
	if err := repo.Create(ctx, replay); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !replay.Applied || replay.ID != o.ID {
		t.Fatalf("replay should reuse offer row: id=%v applied=%v", replay.ID, replay.Applied)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM price_observations").Scan(&obs); err != nil {
		t.Fatal(err)
	}
	if obs != 1 {
		t.Fatalf("replay duplicated observations: %d", obs)
	}
}

func TestCovOfferCreateValidationErrors(t *testing.T) {
	db := covDB(t, true)
	repo := postgres.NewOfferRepo(db)

	neg := int64(-5)
	if err := repo.Create(context.Background(), &domain.Offer{SourceMessageID: "m1", SellerID: 1, PriceAmount: &neg, PriceCurrency: domain.CurrencyToman}); err == nil || !strings.Contains(err.Error(), "negative offer price") {
		t.Fatalf("expected negative price error, got %v", err)
	}
	if err := repo.Create(context.Background(), &domain.Offer{SourceMessageID: "m2", SellerID: 1, PriceCurrency: "USD"}); err == nil || !strings.Contains(err.Error(), "unsupported currency") {
		t.Fatalf("expected unsupported currency error, got %v", err)
	}
}

func TestCovOfferCreateUpsertError(t *testing.T) {
	db := covDB(t, true)
	repo := postgres.NewOfferRepo(db)
	// source_message_id exceeds VARCHAR(100): the upsert itself must fail.
	longID := strings.Repeat("x", 120)
	if err := repo.Create(context.Background(), &domain.Offer{SourceMessageID: longID, SellerID: 1}); err == nil || !strings.Contains(err.Error(), "upsert offer") {
		t.Fatalf("expected upsert error, got %v", err)
	}
}

func TestCovOfferCreateRevisionBranches(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewOfferRepo(db)
	covProduct(t, db, covProductA, "Rev", "Apple", "M", "C")

	// Legacy item key: no product id, brand+model only.
	legacyPrice := int64(50)
	legacy := &domain.Offer{SourceMessageID: "legacy-msg", SellerID: 7, Brand: "Sony", Model: "X1", PriceAmount: &legacyPrice, SourceRevisionAt: covNow}
	if err := repo.Create(ctx, legacy); err != nil {
		t.Fatalf("legacy create: %v", err)
	}
	if legacy.SourceItemKey != "legacy:sony:x1" {
		t.Fatalf("legacy item key = %q", legacy.SourceItemKey)
	}
	// No product id: no price observation is recorded.
	var obs int
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM price_observations").Scan(&obs); err != nil {
		t.Fatal(err)
	}
	if obs != 0 {
		t.Fatalf("legacy offer must not record observations, got %d", obs)
	}

	// Non-zero revision: revision hash includes the unix nano prefix.
	covOfferForCreate(t, db, covOfferID1, covProductA, "rev-msg", 100)
	var revision string
	if err := db.Pool.QueryRow(ctx, "SELECT revision_hash FROM price_observations").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(revision, strconv.FormatInt(covNow.UnixNano(), 10)+":") {
		t.Fatalf("expected prefixed revision hash, got %q", revision)
	}

	// Stale revision: older SourceRevisionAt must not overwrite newer state.
	prod := covProductA
	stale := &domain.Offer{ProductID: &prod, SourceMessageID: "rev-msg", SellerID: 7, Quantity: 1, PriceAmount: priceOf(999), PriceCurrency: domain.CurrencyToman, SourceRevisionAt: covNow.Add(-time.Hour)}
	if err := repo.Create(ctx, stale); err != nil {
		t.Fatalf("stale create: %v", err)
	}
	if stale.Applied {
		t.Fatal("stale revision must not apply")
	}
	// Row still holds the newer price.
	var stored int64
	if err := db.Pool.QueryRow(ctx, "SELECT price_amount FROM offers WHERE id=$1", covOfferID1).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 100 {
		t.Fatalf("stale revision overwrote price: %d", stored)
	}
	// No new observation from the stale revision.
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM price_observations").Scan(&obs); err != nil {
		t.Fatal(err)
	}
	if obs != 1 {
		t.Fatalf("stale revision recorded observations: %d", obs)
	}
}

func TestCovOfferCreateRevisionAdvance(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewOfferRepo(db)
	covProduct(t, db, covProductA, "Adv", "Apple", "M", "C")

	covOfferForCreate(t, db, covOfferID1, covProductA, "adv-msg", 100)

	// Newer revision with a different price must update the row and record a
	// new observation under the new revision hash.
	prod := covProductA
	next := &domain.Offer{ProductID: &prod, SourceMessageID: "adv-msg", SellerID: 9001, Quantity: 1, PriceAmount: priceOf(200), PriceCurrency: domain.CurrencyToman, SourceRevisionAt: covNow.Add(time.Hour)}
	if err := repo.Create(ctx, next); err != nil {
		t.Fatalf("advance create: %v", err)
	}
	if !next.Applied || next.ID != covOfferID1 {
		t.Fatalf("advance should upsert existing row: %+v", next)
	}
	var stored int64
	if err := db.Pool.QueryRow(ctx, "SELECT price_amount FROM offers WHERE id=$1", covOfferID1).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 200 {
		t.Fatalf("price not advanced: %d", stored)
	}
	var obs int
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM price_observations").Scan(&obs); err != nil {
		t.Fatal(err)
	}
	if obs != 2 {
		t.Fatalf("expected 2 observations after advance, got %d", obs)
	}
}

func TestCovOfferGetByID(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	covProduct(t, db, covProductA, "Get Product", "Apple", "M", "C")
	created := covOfferForCreate(t, db, covOfferID1, covProductA, "get-msg", 42)

	got, err := postgres.NewOfferRepo(db).GetByID(ctx, covOfferID1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != created.ID || got.SellerID != 9001 {
		t.Fatalf("unexpected offer: %+v", got)
	}
	if got.SourceMessageID != "get-msg" || got.Condition != domain.ConditionUnknown || got.Quantity != 1 {
		t.Fatalf("offer fields mismatch: %+v", got)
	}
	if got.Product == nil || got.Product.ID != covProductA {
		t.Fatalf("expected joined product, got %+v", got.Product)
	}

	missing, err := postgres.NewOfferRepo(db).GetByID(ctx, uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing offer, got %+v", missing)
	}
}

func TestCovOfferGetByIDError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE offers CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewOfferRepo(db).GetByID(context.Background(), covOfferID1); err == nil {
		t.Fatal("expected error after dropping offers table")
	}
}

func TestCovOfferListFiltersSortPagination(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewOfferRepo(db)
	covProduct(t, db, covProductA, "A", "Apple", "A1", "C")
	covProduct(t, db, covProductB, "B", "BrandB", "B1", "C")
	owner := covOwnerID
	covUser(t, db, owner, "owner")

	covOfferForCreate(t, db, covOfferID1, covProductA, "l-1", 100) // seller 9001
	// Second offer: different seller, owner, model, condition, price.
	priceB := int64(300)
	offerB := &domain.Offer{
		ID: covOfferID2, ProductID: &covProductB, SourceMessageID: "l-2",
		SellerID: 4242, OwnerID: &owner, Brand: "BrandB", Model: "B1", Condition: domain.ConditionUsedGood,
		PriceAmount: &priceB, PriceCurrency: domain.CurrencyToman,
		SourceRevisionAt: covNow,
	}
	if err := repo.Create(ctx, offerB); err != nil {
		t.Fatalf("create offerB: %v", err)
	}
	// Third: rial price 2000 rial = 200 toman.
	priceC := int64(2000)
	offerC := &domain.Offer{
		ID: uuid.New(), ProductID: &covProductA, SourceMessageID: "l-3",
		SellerID: 9001, Model: "A1", Condition: domain.ConditionNew,
		PriceAmount: &priceC, PriceCurrency: domain.CurrencyRial,
		SourceRevisionAt: covNow,
	}
	if err := repo.Create(ctx, offerC); err != nil {
		t.Fatalf("create offerC: %v", err)
	}
	// Fourth: expired listing.
	priceD := int64(50)
	offerD := &domain.Offer{
		ID: uuid.New(), ProductID: &covProductA, SourceMessageID: "l-4",
		SellerID: 9001, Model: "A1",
		PriceAmount: &priceD, PriceCurrency: domain.CurrencyToman,
		ListingStatus: domain.ListingStatusExpired, SourceRevisionAt: covNow,
	}
	if err := repo.Create(ctx, offerD); err != nil {
		t.Fatalf("create offerD: %v", err)
	}

	// Baseline: all four visible without a status filter.
	all, total, err := repo.List(ctx, postgres.OfferFilter{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if total != 4 || len(all) != 4 {
		t.Fatalf("want 4 offers, got %d / total %d", len(all), total)
	}

	// Product filter.
	f := postgres.OfferFilter{ProductID: &covProductA}
	got, total, err := repo.List(ctx, f)
	if err != nil || total != 3 || len(got) != 3 {
		t.Fatalf("product filter: %d / %d / %v", len(got), total, err)
	}
	// Seller filter.
	f = postgres.OfferFilter{SellerID: int64ptr(4242)}
	got, total, err = repo.List(ctx, f)
	if err != nil || total != 1 || got[0].ID != covOfferID2 {
		t.Fatalf("seller filter: %+v / %d / %v", got, total, err)
	}
	// Owner filter.
	f = postgres.OfferFilter{OwnerID: &owner}
	got, total, err = repo.List(ctx, f)
	if err != nil || total != 1 || got[0].ID != covOfferID2 {
		t.Fatalf("owner filter: %+v / %d / %v", got, total, err)
	}
	// Brand filter.
	f = postgres.OfferFilter{Brand: "brandb"}
	got, total, err = repo.List(ctx, f)
	if err != nil || total != 1 || got[0].ID != covOfferID2 {
		t.Fatalf("brand filter: %+v / %d / %v", got, total, err)
	}
	// Model filter.
	f = postgres.OfferFilter{Model: "B1"}
	got, total, err = repo.List(ctx, f)
	if err != nil || total != 1 || got[0].ID != covOfferID2 {
		t.Fatalf("model filter: %+v / %d / %v", got, total, err)
	}
	// Condition filter.
	f = postgres.OfferFilter{Condition: string(domain.ConditionUsedGood)}
	got, total, err = repo.List(ctx, f)
	if err != nil || total != 1 || got[0].ID != covOfferID2 {
		t.Fatalf("condition filter: %+v / %d / %v", got, total, err)
	}
	// Explicit active status: expired offer excluded (3 left).
	f = postgres.OfferFilter{ListingStatus: "active"}
	got, total, err = repo.List(ctx, f)
	if err != nil || total != 3 || len(got) != 3 {
		t.Fatalf("active filter: %d / %d / %v", len(got), total, err)
	}
	// Explicit expired status.
	f = postgres.OfferFilter{ListingStatus: "expired"}
	got, total, err = repo.List(ctx, f)
	if err != nil || total != 1 || got[0].ID != offerD.ID {
		t.Fatalf("expired filter: %+v / %d / %v", got, total, err)
	}
	// Min price (toman-normalized): 100, 200, 300 -> min 200 keeps 2.
	f = postgres.OfferFilter{MinPrice: int64ptr(200)}
	got, total, err = repo.List(ctx, f)
	if err != nil || total != 2 || len(got) != 2 {
		t.Fatalf("min price filter: %d / %d / %v", len(got), total, err)
	}
	// Max price: max 80 keeps only the expired 50-toman offer.
	f = postgres.OfferFilter{MaxPrice: int64ptr(80)}
	got, total, err = repo.List(ctx, f)
	if err != nil || total != 1 || got[0].ID != offerD.ID {
		t.Fatalf("max price filter: %+v / %d / %v", got, total, err)
	}
	// Sort by lowest price (toman-normalized, NULLS LAST): 100, 200(rial), 300, then expired 50?
	// The expired offer has price 50 < 100, so it sorts first.
	f = postgres.OfferFilter{SortBy: domain.SortByLowestPrice, Unbounded: true}
	got, _, err = repo.List(ctx, f)
	if err != nil || len(got) != 4 {
		t.Fatalf("lowest sort: %v", err)
	}
	if got[0].ID != offerD.ID || got[1].ID != covOfferID1 || got[2].ID != offerC.ID || got[3].ID != covOfferID2 {
		t.Fatalf("lowest price order mismatch: %v", ids(got))
	}
	// Sort by highest price.
	f = postgres.OfferFilter{SortBy: domain.SortByHighestPrice, Unbounded: true}
	got, _, err = repo.List(ctx, f)
	if err != nil || len(got) != 4 {
		t.Fatalf("highest sort: %v", err)
	}
	if got[0].ID != covOfferID2 || got[3].ID != offerD.ID {
		t.Fatalf("highest price order mismatch: %v", ids(got))
	}
	// Sort by confidence (all 0.9 -> stable by id ASC).
	f = postgres.OfferFilter{SortBy: domain.SortByConfidence, Unbounded: true}
	got, _, err = repo.List(ctx, f)
	if err != nil || len(got) != 4 {
		t.Fatalf("confidence sort: %v", err)
	}
	// Default limit path: with 4 rows and default limit 20 all visible; with
	// explicit limit 1 + offset 1 only the second row.
	f = postgres.OfferFilter{SortBy: domain.SortByLowestPrice, Limit: 1, Offset: 1}
	got, total, err = repo.List(ctx, f)
	if err != nil || total != 4 || len(got) != 1 || got[0].ID != covOfferID1 {
		t.Fatalf("pagination: %+v / %d / %v", got, total, err)
	}
	// Negative offset resets to zero.
	f = postgres.OfferFilter{SortBy: domain.SortByLowestPrice, Limit: 1, Offset: -3}
	got, _, err = repo.List(ctx, f)
	if err != nil || len(got) != 1 || got[0].ID != offerD.ID {
		t.Fatalf("negative offset: %+v / %v", got, err)
	}
}

func TestCovOfferListCountError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE offers CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := postgres.NewOfferRepo(db).List(context.Background(), postgres.OfferFilter{}); err == nil {
		t.Fatal("expected count error after dropping offers table")
	}
}

func TestCovOfferGetActiveOffersByProductID(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewOfferRepo(db)
	covProduct(t, db, covProductA, "P", "Apple", "M", "C")
	active := covOfferForCreate(t, db, covOfferID1, covProductA, "act-1", 100)
	// Active but expired via listing_expires_at in the past.
	expiredAt := covNow.Add(-time.Hour)
	price := int64(80)
	expired := &domain.Offer{
		ID: uuid.New(), ProductID: &covProductA, SourceMessageID: "act-2",
		SellerID: 1, PriceAmount: &price, ListingExpiresAt: &expiredAt, SourceRevisionAt: covNow,
	}
	if err := repo.Create(ctx, expired); err != nil {
		t.Fatal(err)
	}
	// Active but past expiration_date.
	expDate := covNow.Add(-time.Hour)
	price2 := int64(90)
	expired2 := &domain.Offer{
		ID: uuid.New(), ProductID: &covProductA, SourceMessageID: "act-3",
		SellerID: 1, PriceAmount: &price2, ExpirationDate: &expDate, SourceRevisionAt: covNow,
	}
	if err := repo.Create(ctx, expired2); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetActiveOffersByProductID(ctx, covProductA)
	if err != nil {
		t.Fatalf("active by product: %v", err)
	}
	if len(got) != 1 || got[0].ID != active.ID {
		t.Fatalf("expected only the eligible offer, got %v", ids(got))
	}

	empty, err := repo.GetActiveOffersByProductID(ctx, uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected no offers for unknown product, got %v", ids(empty))
	}
}

func TestCovOfferRecordPriceObservation(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewOfferRepo(db)
	covProduct(t, db, covProductA, "P", "Apple", "M", "C")
	covOfferForCreate(t, db, covOfferID1, covProductA, "obs-msg", 100)

	obs := &domain.PriceObservation{
		ProductID:     covProductA,
		OfferID:       covOfferID1,
		PriceAmount:   120,
		PriceCurrency: domain.CurrencyToman,
		ObservedAt:    covNow,
	}
	if err := repo.RecordPriceObservation(ctx, obs); err != nil {
		t.Fatalf("record: %v", err)
	}
	if obs.ID == uuid.Nil {
		t.Fatal("expected generated observation id")
	}
	var got domain.PriceObservation
	if err := db.Pool.QueryRow(ctx, `SELECT id, product_id, offer_id, price_amount, price_currency, observed_at, created_at FROM price_observations WHERE id=$1`, obs.ID).Scan(
		&got.ID, &got.ProductID, &got.OfferID, &got.PriceAmount, &got.PriceCurrency, &got.ObservedAt, &got.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if got.PriceAmount != 120 || !got.ObservedAt.Equal(covNow) {
		t.Fatalf("observation mismatch: %+v", got)
	}

	// FK violation on product.
	if err := repo.RecordPriceObservation(ctx, &domain.PriceObservation{ProductID: uuid.New(), OfferID: covOfferID1, PriceAmount: 1}); err == nil {
		t.Fatal("expected FK violation for unknown product")
	}
}

func TestCovOfferCountTotalActive(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewOfferRepo(db)
	covProduct(t, db, covProductA, "P", "Apple", "M", "C")

	eligible := covOfferForCreate(t, db, covOfferID1, covProductA, "cnt-1", 100)
	// Expired via listing status.
	price := int64(50)
	expired := &domain.Offer{ID: uuid.New(), ProductID: &covProductA, SourceMessageID: "cnt-2", SellerID: 1, PriceAmount: &price, ListingStatus: domain.ListingStatusSold, SourceRevisionAt: covNow}
	if err := repo.Create(ctx, expired); err != nil {
		t.Fatal(err)
	}
	// Active but past listing_expires_at.
	past := covNow.Add(-time.Hour)
	price2 := int64(60)
	late := &domain.Offer{ID: uuid.New(), ProductID: &covProductA, SourceMessageID: "cnt-3", SellerID: 1, PriceAmount: &price2, ListingExpiresAt: &past, SourceRevisionAt: covNow}
	if err := repo.Create(ctx, late); err != nil {
		t.Fatal(err)
	}

	n, err := repo.CountTotalActive(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 eligible active offer (%s), got %d", eligible.ID, n)
	}
}

func TestCovOfferRetireMissing(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewOfferRepo(db)
	covProduct(t, db, covProductA, "P", "Apple", "M", "C")

	// Two items under the same source message; revision advances for one only.
	keepPrice, dropPrice := int64(100), int64(200)
	keepKey, dropKey := "item-keep", "item-drop"
	base := covNow
	if err := repo.Create(ctx, &domain.Offer{
		ProductID: &covProductA, SourceMessageID: "retire", SellerID: 1,
		SourceItemKey: keepKey, SourceRevisionAt: base, PriceAmount: &keepPrice,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, &domain.Offer{
		ProductID: &covProductA, SourceMessageID: "retire", SellerID: 1,
		SourceItemKey: dropKey, SourceRevisionAt: base, PriceAmount: &dropPrice,
	}); err != nil {
		t.Fatal(err)
	}
	// The surviving item is refreshed to a newer revision.
	nextRev := covNow.Add(time.Hour)
	if err := repo.Create(ctx, &domain.Offer{
		ProductID: &covProductA, SourceMessageID: "retire", SellerID: 1,
		SourceItemKey: keepKey, SourceRevisionAt: nextRev, PriceAmount: priceOf(150),
	}); err != nil {
		t.Fatal(err)
	}

	if err := repo.RetireMissing(ctx, "retire", nextRev, []string{keepKey}); err != nil {
		t.Fatalf("retire: %v", err)
	}
	var keepStatus, dropStatus string
	if err := db.Pool.QueryRow(ctx, "SELECT listing_status FROM offers WHERE source_message_id='retire' AND source_item_key=$1", keepKey).Scan(&keepStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT listing_status FROM offers WHERE source_message_id='retire' AND source_item_key=$1", dropKey).Scan(&dropStatus); err != nil {
		t.Fatal(err)
	}
	if keepStatus != "active" {
		t.Fatalf("kept item must stay active, got %q", keepStatus)
	}
	if dropStatus != "expired" {
		t.Fatalf("missing item must be expired, got %q", dropStatus)
	}
}

func ids(offers []domain.Offer) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(offers))
	for _, o := range offers {
		out = append(out, o.ID)
	}
	return out
}

func priceOf(v int64) *int64 { return &v }

func int64ptr(v int64) *int64 { return &v }
