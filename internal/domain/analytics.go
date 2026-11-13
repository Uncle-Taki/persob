package domain

import (
	"time"

	"github.com/google/uuid"
)

type PriceStatistics struct {
	ProductID      uuid.UUID     `json:"product_id"`
	ProductName    string        `json:"product_name"`
	Currency       Currency      `json:"currency"`
	MinPrice       int64         `json:"min_price"`
	MaxPrice       int64         `json:"max_price"`
	AvgPrice       int64         `json:"avg_price"`
	MedianPrice    int64         `json:"median_price"`
	PriceRange     int64         `json:"price_range"`
	SampleCount    int           `json:"sample_count"`
	PriceTrend     string        `json:"price_trend"` // "rising", "falling", "stable", "insufficient_data"
	Distribution   []PriceBucket `json:"distribution"`
	Methodology    string        `json:"methodology"`
	LastCalculated time.Time     `json:"last_calculated"`
}

type PriceBucket struct {
	RangeStart int64 `json:"range_start"`
	RangeEnd   int64 `json:"range_end"`
	Count      int   `json:"count"`
}

type ScoreBreakdown struct {
	PriceCompetitiveness float64 `json:"price_competitiveness"` // 0.0 to 1.0 (weight: 35%)
	ProductCondition     float64 `json:"product_condition"`     // 0.0 to 1.0 (weight: 20%)
	OfferFreshness       float64 `json:"offer_freshness"`       // 0.0 to 1.0 (weight: 15%)
	ExtractionConfidence float64 `json:"extraction_confidence"` // 0.0 to 1.0 (weight: 10%)
	VariantMatch         float64 `json:"variant_match"`         // 0.0 to 1.0 (weight: 10%)
	LocationMatch        float64 `json:"location_match"`        // 0.0 to 1.0 (weight: 10%)
	FinalScore           float64 `json:"final_score"`           // 0.0 to 100.0
	Explanation          string  `json:"explanation"`
}

type ScoredOffer struct {
	Offer          Offer          `json:"offer"`
	ScoreBreakdown ScoreBreakdown `json:"score_breakdown"`
}

type DashboardSummary struct {
	TotalTrackedProducts int64 `json:"total_tracked_products"`
	TotalActiveOffers    int64 `json:"total_active_offers"`
	TotalWatchSources    int64 `json:"total_watch_sources"`
	MessagesProcessed24h int64 `json:"messages_processed_24h"`
	ActiveConversations  int64 `json:"active_conversations"`
}

type SortBy string

const (
	SortByLowestPrice  SortBy = "lowest_price"
	SortByHighestPrice SortBy = "highest_price"
	SortByNewest       SortBy = "newest"
	SortByBestOffer    SortBy = "best_offer"
	SortByConfidence   SortBy = "confidence"
	SortByFreshness    SortBy = "freshness"
)

type ConditionComparison struct {
	Condition ItemCondition `json:"condition"`
	AvgPrice  int64         `json:"avg_price"`
	MinPrice  int64         `json:"min_price"`
	MaxPrice  int64         `json:"max_price"`
	Count     int           `json:"count"`
}

type CityComparison struct {
	City     string `json:"city"`
	AvgPrice int64  `json:"avg_price"`
	MinPrice int64  `json:"min_price"`
	MaxPrice int64  `json:"max_price"`
	Count    int    `json:"count"`
}

type BrandComparison struct {
	Brand    string `json:"brand"`
	AvgPrice int64  `json:"avg_price"`
	MinPrice int64  `json:"min_price"`
	MaxPrice int64  `json:"max_price"`
	Count    int    `json:"count"`
}
