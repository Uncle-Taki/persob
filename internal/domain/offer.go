package domain

import (
	"time"

	"github.com/google/uuid"
)

type ItemCondition string

const (
	ConditionNew      ItemCondition = "new"
	ConditionLikeNew  ItemCondition = "like_new"
	ConditionUsedGood ItemCondition = "used_good"
	ConditionUsedFair ItemCondition = "used_fair"
	ConditionUsedPoor ItemCondition = "used_poor"
	ConditionUnknown  ItemCondition = "unknown"
)

type ListingStatus string

const (
	ListingStatusActive     ListingStatus = "active"
	ListingStatusSold       ListingStatus = "sold"
	ListingStatusExpired    ListingStatus = "expired"
	ListingStatusUnverified ListingStatus = "unverified"
)

type Currency string

const (
	CurrencyToman Currency = "IRR_TOMAN"
	CurrencyRial  Currency = "IRR_RIAL"
)

type Offer struct {
	ID                   uuid.UUID     `json:"id"`
	ProductID            *uuid.UUID    `json:"product_id,omitempty"`
	SourceMessageID      string        `json:"source_message_id"`
	SellerID             int64         `json:"seller_id"`
	Brand                string        `json:"brand,omitempty"`
	Manufacturer         string        `json:"manufacturer,omitempty"`
	Model                string        `json:"model,omitempty"`
	Condition            ItemCondition `json:"condition"`
	Quantity             int           `json:"quantity"`
	PriceAmount          *int64        `json:"price_amount,omitempty"`
	PriceCurrency        Currency      `json:"price_currency"`
	PriceRaw             string        `json:"price_raw,omitempty"`
	PriceConfidence      float64       `json:"price_confidence"`
	Branded              bool          `json:"branded"`
	ExpirationDate       *time.Time    `json:"expiration_date,omitempty"`
	SellerCity           string        `json:"seller_city,omitempty"`
	SellerArea           string        `json:"seller_area,omitempty"`
	SellerLocationText   string        `json:"seller_location_text,omitempty"`
	ListingStatus        ListingStatus `json:"listing_status"`
	ExtractionConfidence float64       `json:"extraction_confidence"`
	CreatedAt            time.Time     `json:"created_at"`
	UpdatedAt            time.Time     `json:"updated_at"`

	// Relational population
	Product *Product `json:"product,omitempty"`
}

type PriceObservation struct {
	ID            uuid.UUID `json:"id"`
	ProductID     uuid.UUID `json:"product_id"`
	OfferID       uuid.UUID `json:"offer_id"`
	PriceAmount   int64     `json:"price_amount"`
	PriceCurrency Currency  `json:"price_currency"`
	ObservedAt    time.Time `json:"observed_at"`
	CreatedAt     time.Time `json:"created_at"`
}

type OfferMatchResult struct {
	ID                  uuid.UUID              `json:"id"`
	OfferID             uuid.UUID              `json:"offer_id"`
	InterestID          *uuid.UUID             `json:"interest_id,omitempty"`
	UserID              uuid.UUID              `json:"user_id"`
	ProductID           uuid.UUID              `json:"product_id"`
	MatchScore          float64                `json:"match_score"`
	MatchExplanation    string                 `json:"match_explanation,omitempty"`
	MatchedAttributes   map[string]interface{} `json:"matched_attributes"`
	UnmatchedAttributes map[string]interface{} `json:"unmatched_attributes"`
	CreatedAt           time.Time              `json:"created_at"`
}
