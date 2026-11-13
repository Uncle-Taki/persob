package analytics_test

import (
	"testing"
	"time"

	"github.com/perso/perso-mind/internal/analytics"
	"github.com/perso/perso-mind/internal/domain"
)

func TestScoreOffer(t *testing.T) {
	price := int64(80_000_000)
	medianPrice := int64(100_000_000) // price is 20% below median -> top price score

	offer := domain.Offer{
		PriceAmount:          &price,
		PriceCurrency:        domain.CurrencyToman,
		Condition:            domain.ConditionLikeNew,
		CreatedAt:            time.Now().Add(-2 * time.Hour), // Very fresh (< 6h)
		ExtractionConfidence: 0.95,
		SellerCity:           "تهران",
		SellerArea:           "علاءالدین",
		Model:                "15 Pro",
	}

	score := analytics.ScoreOffer(offer, medianPrice)

	if score.FinalScore < 75.0 {
		t.Errorf("Expected high final score for competitive fresh offer, got %f", score.FinalScore)
	}

	if score.PriceCompetitiveness < 0.9 {
		t.Errorf("Expected price competitiveness >= 0.9, got %f", score.PriceCompetitiveness)
	}

	if score.OfferFreshness < 0.9 {
		t.Errorf("Expected freshness >= 0.9, got %f", score.OfferFreshness)
	}

	if score.Explanation == "" {
		t.Errorf("Expected non-empty explainable rationale string")
	}
}
