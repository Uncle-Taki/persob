package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

func TestCovAnalyticsUpsertAndGetProductAggregate(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewAnalyticsRepo(db)
	covProduct(t, db, covProductA, "Phone", "Apple", "M", "C")

	agg := &domain.PriceStatistics{
		ProductID: covProductA, MinPrice: 100, MaxPrice: 300, AvgPrice: 200,
		MedianPrice: 200, PriceRange: 200, SampleCount: 3, Currency: domain.CurrencyToman,
	}
	if err := repo.UpsertAggregate(ctx, agg); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := repo.GetProductAggregate(ctx, covProductA, domain.CurrencyToman)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ProductName != "Phone" || got.MinPrice != 100 || got.MaxPrice != 300 || got.AvgPrice != 200 {
		t.Fatalf("aggregate mismatch: %+v", got)
	}
	if got.SampleCount != 3 || got.Currency != domain.CurrencyToman {
		t.Fatalf("aggregate mismatch: %+v", got)
	}
	if got.Methodology == "" {
		t.Fatal("expected methodology to be set")
	}
	if got.LastCalculated.IsZero() {
		t.Fatal("expected last_calculated set")
	}

	// Re-upsert updates the same row.
	agg2 := &domain.PriceStatistics{ProductID: covProductA, MinPrice: 150, MaxPrice: 350, AvgPrice: 250, MedianPrice: 250, PriceRange: 200, SampleCount: 4, Currency: domain.CurrencyToman}
	if err := repo.UpsertAggregate(ctx, agg2); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got2, err := repo.GetProductAggregate(ctx, covProductA, domain.CurrencyToman)
	if err != nil {
		t.Fatal(err)
	}
	if got2.MinPrice != 150 || got2.SampleCount != 4 {
		t.Fatalf("re-upsert did not update: %+v", got2)
	}
	var n int
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM price_aggregates").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected a single aggregate row, got %d", n)
	}
}

func TestCovAnalyticsGetProductAggregateNotFound(t *testing.T) {
	db := covDB(t, true)
	covProduct(t, db, covProductA, "Phone", "Apple", "M", "C")
	if _, err := postgres.NewAnalyticsRepo(db).GetProductAggregate(context.Background(), covProductA, domain.CurrencyToman); err == nil {
		t.Fatal("expected no-rows error when no aggregate exists")
	}
	// Unknown product too.
	if _, err := postgres.NewAnalyticsRepo(db).GetProductAggregate(context.Background(), uuid.New(), domain.CurrencyToman); err == nil {
		t.Fatal("expected error for unknown product")
	}
}

func TestCovAnalyticsGetHistoricalPrices(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewAnalyticsRepo(db)
	covProduct(t, db, covProductA, "Phone", "Apple", "M", "C")
	covOfferForCreate(t, db, covOfferID1, covProductA, "hist-1", 100)

	obs := &domain.PriceObservation{ProductID: covProductA, OfferID: covOfferID1, PriceAmount: 100, PriceCurrency: domain.CurrencyToman, ObservedAt: covNow}
	if err := postgres.NewOfferRepo(db).RecordPriceObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	// Since before the observations: the offer create recorded one (at covNow)
	// and the explicit one above at covNow -> two total.
	got, err := repo.GetHistoricalPrices(ctx, covProductA, covNow.Add(-time.Hour))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("history mismatch: %+v", got)
	}
	for _, o := range got {
		if o.PriceAmount != 100 || !o.ObservedAt.Equal(covNow) {
			t.Fatalf("observation mismatch: %+v", o)
		}
	}
	// Since after: empty.
	empty, err := repo.GetHistoricalPrices(ctx, covProductA, covNow.Add(time.Hour))
	if err != nil {
		t.Fatalf("empty history: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected no observations after cutoff, got %+v", empty)
	}
	// Unknown product.
	none, err := repo.GetHistoricalPrices(ctx, uuid.New(), covNow.Add(-time.Hour))
	if err != nil || len(none) != 0 {
		t.Fatalf("expected none: %v / %v", none, err)
	}
}

func TestCovAnalyticsGetHistoricalPricesError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE price_observations CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewAnalyticsRepo(db).GetHistoricalPrices(context.Background(), covProductA, covNow); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovAnalyticsGetDashboardSummary(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewAnalyticsRepo(db)
	covUser(t, db, covBuyerID, "buyer")
	covProduct(t, db, covProductA, "P", "Apple", "M", "C")
	covProduct(t, db, covProductB, "Q", "Brand", "M2", "C")
	covOfferForCreate(t, db, covOfferID1, covProductA, "dash-1", 100)

	// A paused watch source (not active -> excluded).
	if err := postgres.NewWatchSourceRepo(db).Create(ctx, &domain.WatchSource{UserID: covBuyerID, TelegramChatID: -1, Title: "w", MonitoringStatus: domain.MonitoringStatusPaused}); err != nil {
		t.Fatal(err)
	}

	summary, err := repo.GetDashboardSummary(ctx)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.TotalTrackedProducts != 2 {
		t.Fatalf("products = %d, want 2", summary.TotalTrackedProducts)
	}
	if summary.TotalActiveOffers != 1 {
		t.Fatalf("active offers = %d, want 1", summary.TotalActiveOffers)
	}
	if summary.TotalWatchSources != 0 {
		t.Fatalf("watch sources = %d, want 0 (paused)", summary.TotalWatchSources)
	}
	if summary.ActiveConversations != 0 {
		t.Fatalf("active conversations = %d, want 0", summary.ActiveConversations)
	}
}

func TestCovAnalyticsGetConditionComparison(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewAnalyticsRepo(db)
	covProduct(t, db, covProductA, "P", "Apple", "M", "C")
	covProduct(t, db, covProductB, "Q", "Brand", "M2", "C")

	// New: 100 toman. Used: 200 toman.
	newP, usedP := int64(100), int64(200)
	if err := postgres.NewOfferRepo(db).Create(ctx, &domain.Offer{ID: covOfferID1, ProductID: &covProductA, SourceMessageID: "cc-1", SellerID: 1, PriceAmount: &newP, Condition: domain.ConditionNew, SourceRevisionAt: covNow}); err != nil {
		t.Fatal(err)
	}
	if err := postgres.NewOfferRepo(db).Create(ctx, &domain.Offer{ID: covOfferID2, ProductID: &covProductA, SourceMessageID: "cc-2", SellerID: 1, PriceAmount: &usedP, Condition: domain.ConditionUsedGood, SourceRevisionAt: covNow}); err != nil {
		t.Fatal(err)
	}

	// Filter by product A: two conditions.
	got, err := repo.GetConditionComparison(ctx, &covProductA)
	if err != nil {
		t.Fatalf("comparison: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 conditions, got %d: %+v", len(got), got)
	}
	// Ordered by avg_price ASC: new (100) first.
	if got[0].Condition != domain.ConditionNew || got[0].AvgPrice != 100 {
		t.Fatalf("order mismatch: %+v", got)
	}
	if got[1].Condition != domain.ConditionUsedGood || got[1].AvgPrice != 200 {
		t.Fatalf("order mismatch: %+v", got)
	}

	// No product filter: same two rows (both offers belong to A).
	all, err := repo.GetConditionComparison(ctx, nil)
	if err != nil {
		t.Fatalf("comparison all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 rows unfiltered, got %d", len(all))
	}

	// Unknown product: empty.
	none, err := repo.GetConditionComparison(ctx, &covProductB)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("expected none for product B, got %+v", none)
	}
}

func TestCovAnalyticsGetConditionComparisonError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE offers CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewAnalyticsRepo(db).GetConditionComparison(context.Background(), nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovAnalyticsGetCityComparison(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewAnalyticsRepo(db)
	covProduct(t, db, covProductA, "P", "Apple", "M", "C")

	// Tehran 100, Shiraz 200, and one with no city (nameless).
	t1, t2, t3 := int64(100), int64(200), int64(300)
	if err := postgres.NewOfferRepo(db).Create(ctx, &domain.Offer{ID: covOfferID1, ProductID: &covProductA, SourceMessageID: "city-1", SellerID: 1, PriceAmount: &t1, SellerCity: "Tehran", SourceRevisionAt: covNow}); err != nil {
		t.Fatal(err)
	}
	if err := postgres.NewOfferRepo(db).Create(ctx, &domain.Offer{ID: covOfferID2, ProductID: &covProductA, SourceMessageID: "city-2", SellerID: 1, PriceAmount: &t2, SellerCity: "Shiraz", SourceRevisionAt: covNow}); err != nil {
		t.Fatal(err)
	}
	if err := postgres.NewOfferRepo(db).Create(ctx, &domain.Offer{ID: uuid.New(), ProductID: &covProductA, SourceMessageID: "city-3", SellerID: 1, PriceAmount: &t3, SellerCity: "", SourceRevisionAt: covNow}); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetCityComparison(ctx, nil)
	if err != nil {
		t.Fatalf("city comparison: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 cities, got %d: %+v", len(got), got)
	}
	// Ordered by count DESC then avg ASC: all count 1, so by avg ASC:
	// Tehran(100), nameless(300)? No - the nameless is 300, Shiraz is 200.
	// So order: Tehran(100), Shiraz(200), nameless(300).
	if got[0].City != "Tehran" || got[0].AvgPrice != 100 {
		t.Fatalf("city order mismatch: %+v", got)
	}
	if got[1].City != "Shiraz" || got[1].AvgPrice != 200 {
		t.Fatalf("city order mismatch: %+v", got)
	}
	if got[2].AvgPrice != 300 {
		t.Fatalf("nameless city mismatch: %+v", got[2])
	}
}

func TestCovAnalyticsGetCityComparisonError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE offers CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewAnalyticsRepo(db).GetCityComparison(context.Background(), nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovAnalyticsGetBrandComparison(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewAnalyticsRepo(db)
	covProduct(t, db, covProductA, "P", "Apple", "M", "C")

	// Two Apple offers, one BrandB offer.
	a1, a2, b1 := int64(100), int64(200), int64(50)
	// covOfferForCreate sets Brand to "Apple".
	if err := postgres.NewOfferRepo(db).Create(ctx, &domain.Offer{ID: covOfferID1, ProductID: &covProductA, SourceMessageID: "br-1", SellerID: 1, Brand: "Apple", PriceAmount: &a1, SourceRevisionAt: covNow}); err != nil {
		t.Fatal(err)
	}
	if err := postgres.NewOfferRepo(db).Create(ctx, &domain.Offer{ID: covOfferID2, ProductID: &covProductA, SourceMessageID: "br-2", SellerID: 1, Brand: "Apple", PriceAmount: &a2, SourceRevisionAt: covNow}); err != nil {
		t.Fatal(err)
	}
	if err := postgres.NewOfferRepo(db).Create(ctx, &domain.Offer{ID: uuid.New(), ProductID: &covProductA, SourceMessageID: "br-3", SellerID: 1, Brand: "BrandB", PriceAmount: &b1, SourceRevisionAt: covNow}); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetBrandComparison(ctx)
	if err != nil {
		t.Fatalf("brand comparison: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 brands, got %d: %+v", len(got), got)
	}
	// Ordered by count DESC: Apple (2) first, then BrandB (1).
	if got[0].Brand != "Apple" || got[0].Count != 2 || got[0].AvgPrice != 150 {
		t.Fatalf("apple brand mismatch: %+v", got[0])
	}
	if got[1].Brand != "BrandB" || got[1].Count != 1 {
		t.Fatalf("brandB mismatch: %+v", got[1])
	}
}

func TestCovAnalyticsGetBrandComparisonError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE offers CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewAnalyticsRepo(db).GetBrandComparison(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
