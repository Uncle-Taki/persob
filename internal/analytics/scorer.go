package analytics

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/perso/perso-mind/internal/domain"
)

// ScoreOffer computes an explainable multi-dimensional score (0-100) for a seller offer
func ScoreOffer(offer domain.Offer, medianPrice int64) domain.ScoreBreakdown {
	// 1. Price Competitiveness (35% weight)
	priceScore := 0.5
	if offer.PriceAmount != nil && *offer.PriceAmount > 0 && medianPrice > 0 {
		ratio := float64(*offer.PriceAmount) / float64(medianPrice)
		if ratio <= 0.80 {
			priceScore = 1.0 // 20%+ cheaper than median
		} else if ratio <= 0.90 {
			priceScore = 0.90
		} else if ratio <= 1.0 {
			priceScore = 0.75
		} else if ratio <= 1.15 {
			priceScore = 0.55
		} else if ratio <= 1.30 {
			priceScore = 0.35
		} else {
			priceScore = 0.15
		}
	} else if offer.PriceAmount == nil {
		priceScore = 0.2 // Missing price penalty
	}

	// 2. Product Condition (20% weight)
	condScore := 0.4
	switch offer.Condition {
	case domain.ConditionNew:
		condScore = 1.0
	case domain.ConditionLikeNew:
		condScore = 0.85
	case domain.ConditionUsedGood:
		condScore = 0.70
	case domain.ConditionUsedFair:
		condScore = 0.50
	case domain.ConditionUsedPoor:
		condScore = 0.20
	case domain.ConditionUnknown:
		condScore = 0.40
	}

	// 3. Freshness (15% weight)
	ageHours := time.Since(offer.CreatedAt).Hours()
	freshScore := 0.2
	switch {
	case ageHours <= 6:
		freshScore = 1.0
	case ageHours <= 24:
		freshScore = 0.85
	case ageHours <= 72:
		freshScore = 0.65
	case ageHours <= 168: // 1 week
		freshScore = 0.45
	default:
		freshScore = 0.20
	}

	// 4. Extraction Confidence (10% weight)
	confScore := offer.ExtractionConfidence
	if confScore <= 0 {
		confScore = 0.5
	}

	// 5. Variant Match (10% weight)
	variantScore := 0.7
	if offer.Model != "" {
		variantScore = 0.9
	}

	// 6. Location Compatibility (10% weight)
	locScore := 0.5
	locLower := strings.ToLower(offer.SellerArea + " " + offer.SellerCity + " " + offer.SellerLocationText)
	if strings.Contains(locLower, "علاءالدین") || strings.Contains(locLower, "چارسو") || strings.Contains(locLower, "بازار") {
		locScore = 1.0 // Central hub
	} else if strings.Contains(locLower, "تهران") {
		locScore = 0.85
	} else if offer.SellerCity != "" {
		locScore = 0.70
	}

	// Weighted sum
	finalScore := (priceScore * 35.0) +
		(condScore * 20.0) +
		(freshScore * 15.0) +
		(confScore * 10.0) +
		(variantScore * 10.0) +
		(locScore * 10.0)

	explanation := fmt.Sprintf(
		"امتیاز کل: %.1f از ۱۰۰ | قیمت: %.0f%% (وزن ۳۵٪)، وضعیت کالا: %s (وزن ۲۰٪)، تازگی: %.1f ساعت پیش (وزن ۱۵٪)، اعتماد استخراج: %.0f%%",
		finalScore,
		priceScore*100,
		offer.Condition,
		ageHours,
		confScore*100,
	)

	return domain.ScoreBreakdown{
		PriceCompetitiveness: math.Round(priceScore*100) / 100,
		ProductCondition:     math.Round(condScore*100) / 100,
		OfferFreshness:       math.Round(freshScore*100) / 100,
		ExtractionConfidence: math.Round(confScore*100) / 100,
		VariantMatch:         math.Round(variantScore*100) / 100,
		LocationMatch:        math.Round(locScore*100) / 100,
		FinalScore:           math.Round(finalScore*10) / 10,
		Explanation:          explanation,
	}
}
