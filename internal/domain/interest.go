package domain

import (
	"time"

	"github.com/google/uuid"
)

type Interest struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	ProductID   uuid.UUID `json:"product_id"`
	SellEnabled bool      `json:"sell_enabled"` // Phase 1: false
	BuyEnabled  bool      `json:"buy_enabled"`  // Phase 1: true
	CreatedAt   time.Time `json:"created_at"`
	Product     *Product  `json:"product,omitempty"`
}

type CreateInterestRequest struct {
	ProductID uuid.UUID `json:"product_id"`
}
