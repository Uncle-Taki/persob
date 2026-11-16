package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Interest struct {
	ID                 uuid.UUID       `json:"id"`
	UserID             uuid.UUID       `json:"user_id"`
	ProductID          uuid.UUID       `json:"product_id"`
	SellEnabled        bool            `json:"sell_enabled"` // Phase 1: false
	BuyEnabled         bool            `json:"buy_enabled"`  // Phase 1: true
	CreatedAt          time.Time       `json:"created_at"`
	Product            *Product        `json:"product,omitempty"`
	MaxPriceAmount     *int64          `json:"max_price_amount,omitempty"`
	MinQuantity        *int            `json:"min_quantity,omitempty"`
	PreferredCity      string          `json:"preferred_city,omitempty"`
	RequiredAttributes json.RawMessage `json:"required_attributes,omitempty"`
}

type CreateInterestRequest struct {
	ProductID          uuid.UUID       `json:"product_id"`
	MaxPriceAmount     *int64          `json:"max_price_amount,omitempty"`
	MinQuantity        *int            `json:"min_quantity,omitempty"`
	PreferredCity      string          `json:"preferred_city,omitempty"`
	RequiredAttributes json.RawMessage `json:"required_attributes,omitempty"`
}

type UpdateInterestRequest struct {
	MaxPriceAmount     *int64          `json:"max_price_amount,omitempty"`
	MinQuantity        *int            `json:"min_quantity,omitempty"`
	PreferredCity      *string         `json:"preferred_city,omitempty"`
	RequiredAttributes json.RawMessage `json:"required_attributes,omitempty"`
}
