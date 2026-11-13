package analytics

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

type Engine struct {
	offerRepo     *postgres.OfferRepo
	productRepo   *postgres.ProductRepo
	analyticsRepo *postgres.AnalyticsRepo
}

func NewEngine(
	offerRepo *postgres.OfferRepo,
	productRepo *postgres.ProductRepo,
	analyticsRepo *postgres.AnalyticsRepo,
) *Engine {
	return &Engine{
		offerRepo:     offerRepo,
		productRepo:   productRepo,
		analyticsRepo: analyticsRepo,
	}
}

// CalculateProductStatistics calculates comprehensive statistics for a product based on active offers
func (e *Engine) CalculateProductStatistics(ctx context.Context, productID uuid.UUID) (*domain.PriceStatistics, error) {
	product, err := e.productRepo.GetByID(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("get product: %w", err)
	}
	if product == nil {
		return nil, fmt.Errorf("product not found: %s", productID)
	}

	offers, err := e.offerRepo.GetActiveOffersByProductID(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("get active offers: %w", err)
	}

	// Extract confirmed Toman prices (excluding zero / nil / unconfirmed)
	var prices []int64
	for _, o := range offers {
		if o.PriceAmount != nil && *o.PriceAmount > 0 && o.PriceCurrency == domain.CurrencyToman {
			prices = append(prices, *o.PriceAmount)
		}
	}

	if len(prices) == 0 {
		return &domain.PriceStatistics{
			ProductID:      productID,
			ProductName:    product.Name,
			Currency:       domain.CurrencyToman,
			SampleCount:    0,
			PriceTrend:     "insufficient_data",
			Methodology:    "No confirmed price observations available",
			LastCalculated: time.Now(),
		}, nil
	}

	// Filter outliers using Interquartile Range (IQR)
	cleanPrices := filterOutliersIQR(prices)
	if len(cleanPrices) == 0 {
		cleanPrices = prices
	}

	sort.Slice(cleanPrices, func(i, j int) bool { return cleanPrices[i] < cleanPrices[j] })

	sampleCount := len(cleanPrices)
	minPrice := cleanPrices[0]
	maxPrice := cleanPrices[sampleCount-1]

	var sum int64
	for _, p := range cleanPrices {
		sum += p
	}
	avgPrice := sum / int64(sampleCount)

	var medianPrice int64
	if sampleCount%2 == 1 {
		medianPrice = cleanPrices[sampleCount/2]
	} else {
		medianPrice = (cleanPrices[(sampleCount/2)-1] + cleanPrices[sampleCount/2]) / 2
	}

	priceRange := maxPrice - minPrice

	// Distribution (5 buckets)
	distribution := computeDistribution(cleanPrices, minPrice, maxPrice, 5)

	// Price trend
	trend := determinePriceTrend(offers)

	stats := &domain.PriceStatistics{
		ProductID:      productID,
		ProductName:    product.Name,
		Currency:       domain.CurrencyToman,
		MinPrice:       minPrice,
		MaxPrice:       maxPrice,
		AvgPrice:       avgPrice,
		MedianPrice:    medianPrice,
		PriceRange:     priceRange,
		SampleCount:    sampleCount,
		PriceTrend:     trend,
		Distribution:   distribution,
		Methodology:    "IQR-filtered confirmed seller advertisements in Iranian Toman (IRR_TOMAN)",
		LastCalculated: time.Now(),
	}

	// Persist aggregate in database
	_ = e.analyticsRepo.UpsertAggregate(ctx, stats)

	return stats, nil
}

// ScoreAndRankOffers calculates explainable scores for all offers and sorts them
func (e *Engine) ScoreAndRankOffers(offers []domain.Offer, medianPrice int64, sortBy domain.SortBy) []domain.ScoredOffer {
	scored := make([]domain.ScoredOffer, len(offers))

	for i, o := range offers {
		breakdown := ScoreOffer(o, medianPrice)
		scored[i] = domain.ScoredOffer{
			Offer:          o,
			ScoreBreakdown: breakdown,
		}
	}

	// Sort
	switch sortBy {
	case domain.SortByBestOffer:
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].ScoreBreakdown.FinalScore > scored[j].ScoreBreakdown.FinalScore
		})
	case domain.SortByLowestPrice:
		sort.Slice(scored, func(i, j int) bool {
			p1 := int64(1<<62 - 1)
			if scored[i].Offer.PriceAmount != nil {
				p1 = *scored[i].Offer.PriceAmount
			}
			p2 := int64(1<<62 - 1)
			if scored[j].Offer.PriceAmount != nil {
				p2 = *scored[j].Offer.PriceAmount
			}
			return p1 < p2
		})
	case domain.SortByHighestPrice:
		sort.Slice(scored, func(i, j int) bool {
			var p1, p2 int64
			if scored[i].Offer.PriceAmount != nil {
				p1 = *scored[i].Offer.PriceAmount
			}
			if scored[j].Offer.PriceAmount != nil {
				p2 = *scored[j].Offer.PriceAmount
			}
			return p1 > p2
		})
	case domain.SortByFreshness, domain.SortByNewest:
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].Offer.CreatedAt.After(scored[j].Offer.CreatedAt)
		})
	case domain.SortByConfidence:
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].Offer.ExtractionConfidence > scored[j].Offer.ExtractionConfidence
		})
	default:
		// Default to Best Offer
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].ScoreBreakdown.FinalScore > scored[j].ScoreBreakdown.FinalScore
		})
	}

	return scored
}

func filterOutliersIQR(prices []int64) []int64 {
	if len(prices) < 4 {
		return prices
	}

	sorted := make([]int64, len(prices))
	copy(sorted, prices)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	n := len(sorted)
	q1 := sorted[n/4]
	q3 := sorted[(3*n)/4]
	iqr := q3 - q1

	lowerBound := q1 - int64(1.5*float64(iqr))
	upperBound := q3 + int64(1.5*float64(iqr))

	var clean []int64
	for _, p := range sorted {
		if p >= lowerBound && p <= upperBound {
			clean = append(clean, p)
		}
	}
	return clean
}

func computeDistribution(prices []int64, min, max int64, numBuckets int) []domain.PriceBucket {
	if numBuckets <= 0 || min == max || len(prices) == 0 {
		return []domain.PriceBucket{
			{RangeStart: min, RangeEnd: max, Count: len(prices)},
		}
	}

	step := (max - min) / int64(numBuckets)
	if step == 0 {
		step = 1
	}

	buckets := make([]domain.PriceBucket, numBuckets)
	for i := 0; i < numBuckets; i++ {
		start := min + int64(i)*step
		end := start + step
		if i == numBuckets-1 {
			end = max
		}
		buckets[i] = domain.PriceBucket{
			RangeStart: start,
			RangeEnd:   end,
			Count:      0,
		}
	}

	for _, p := range prices {
		placed := false
		for i := range buckets {
			if (p >= buckets[i].RangeStart && p < buckets[i].RangeEnd) || (i == numBuckets-1 && p <= buckets[i].RangeEnd) {
				buckets[i].Count++
				placed = true
				break
			}
		}
		if !placed && len(buckets) > 0 {
			buckets[len(buckets)-1].Count++
		}
	}

	return buckets
}

func determinePriceTrend(offers []domain.Offer) string {
	if len(offers) < 3 {
		return "insufficient_data"
	}

	now := time.Now()
	var recentSum, olderSum int64
	var recentCount, olderCount int

	for _, o := range offers {
		if o.PriceAmount == nil || *o.PriceAmount <= 0 {
			continue
		}
		age := now.Sub(o.CreatedAt)
		if age <= 48*time.Hour {
			recentSum += *o.PriceAmount
			recentCount++
		} else {
			olderSum += *o.PriceAmount
			olderCount++
		}
	}

	if recentCount == 0 || olderCount == 0 {
		return "stable"
	}

	recentAvg := float64(recentSum) / float64(recentCount)
	olderAvg := float64(olderSum) / float64(olderCount)

	diffPct := (recentAvg - olderAvg) / olderAvg
	if diffPct > 0.03 {
		return "rising"
	} else if diffPct < -0.03 {
		return "falling"
	}
	return "stable"
}
