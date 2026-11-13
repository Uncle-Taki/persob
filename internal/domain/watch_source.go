package domain

import (
	"time"

	"github.com/google/uuid"
)

type SourceType string

const (
	SourceTypeChannel    SourceType = "channel"
	SourceTypeGroup      SourceType = "group"
	SourceTypeSupergroup SourceType = "supergroup"
)

type MonitoringStatus string

const (
	MonitoringStatusActive MonitoringStatus = "active"
	MonitoringStatusPaused MonitoringStatus = "paused"
	MonitoringStatusError  MonitoringStatus = "error"
)

type WatchSource struct {
	ID               uuid.UUID        `json:"id"`
	UserID           uuid.UUID        `json:"user_id"`
	TelegramChatID   int64            `json:"telegram_chat_id"`
	Username         string           `json:"username,omitempty"`
	Title            string           `json:"title"`
	SourceType       SourceType       `json:"source_type"`
	SellEnabled      bool             `json:"sell_enabled"` // Phase 1: false
	BuyEnabled       bool             `json:"buy_enabled"`  // Phase 1: true
	MonitoringStatus MonitoringStatus `json:"monitoring_status"`
	CreatedAt        time.Time        `json:"created_at"`
}

type CreateWatchSourceRequest struct {
	TelegramChatID int64      `json:"telegram_chat_id"`
	Username       string     `json:"username,omitempty"`
	Title          string     `json:"title"`
	SourceType     SourceType `json:"source_type,omitempty"`
}
