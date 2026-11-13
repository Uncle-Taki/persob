package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Product struct {
	ID             uuid.UUID       `json:"id"`
	Name           string          `json:"name"`
	NormalizedName string          `json:"normalized_name"`
	Brand          string          `json:"brand"`
	Manufacturer   string          `json:"manufacturer,omitempty"`
	Model          string          `json:"model"`
	Category       string          `json:"category"`
	Attributes     json.RawMessage `json:"attributes,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	Aliases        []ProductAlias  `json:"aliases,omitempty"`
}

type ProductAlias struct {
	ID              uuid.UUID `json:"id"`
	ProductID       uuid.UUID `json:"product_id"`
	Alias           string    `json:"alias"`
	NormalizedAlias string    `json:"normalized_alias"`
	Locale          string    `json:"locale"` // "fa", "en"
	AliasType       string    `json:"alias_type"`
	CreatedAt       time.Time `json:"created_at"`
}
