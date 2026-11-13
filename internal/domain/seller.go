package domain

import (
	"time"
)

type SellerProfile struct {
	SellerID      int64      `json:"seller_id"`
	Username      string     `json:"username,omitempty"`
	DisplayName   string     `json:"display_name,omitempty"`
	City          string     `json:"city,omitempty"`
	TehranArea    string     `json:"tehran_area,omitempty"`
	TotalOffers   int        `json:"total_offers"`
	OptedOut      bool       `json:"opted_out"`
	CooldownUntil *time.Time `json:"cooldown_until,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
