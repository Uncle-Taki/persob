package analytics_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/analytics"
	"github.com/perso/perso-mind/internal/domain"
)

// BenchmarkScoreAndRankOffers is the repeatable P1-02 workload. Run it with
// `go test ./internal/analytics -bench BenchmarkScoreAndRankOffers -benchmem`.
// Ten thousand offers approximates a broad workspace catalog and exercises the
// same full-population ranking path used before API pagination.
func BenchmarkScoreAndRankOffers(b *testing.B) {
	offers := make([]domain.Offer, 10_000)
	now := time.Now()
	for i := range offers {
		price := int64(50_000 + i%2_000)
		offers[i] = domain.Offer{
			ID: uuid.New(), PriceAmount: &price, PriceCurrency: domain.CurrencyToman,
			Condition: domain.ConditionNew, CreatedAt: now.Add(-time.Duration(i%720) * time.Hour),
			ExtractionConfidence: 0.9, Model: "benchmark-" + strconv.Itoa(i),
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ranked := (&analytics.Engine{}).ScoreAndRankOffers(offers, 60_000, domain.SortByBestOffer)
		if len(ranked) != len(offers) {
			b.Fatalf("ranked %d offers, want %d", len(ranked), len(offers))
		}
	}
}
