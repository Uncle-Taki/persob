package domain

import (
	"time"

	"github.com/google/uuid"
)

type TelegramSourceType string

const (
	TelegramSourceTypeBasicGroup       TelegramSourceType = "basic_group"
	TelegramSourceTypeSupergroup       TelegramSourceType = "supergroup"
	TelegramSourceTypeForum            TelegramSourceType = "forum"
	TelegramSourceTypeBroadcastChannel TelegramSourceType = "broadcast_channel"
)

type AccessType string

const (
	AccessTypePublic  AccessType = "public"
	AccessTypePrivate AccessType = "private"
)

type IngestionStatus string

const (
	IngestionStatusActive       IngestionStatus = "active"
	IngestionStatusPaused       IngestionStatus = "paused"
	IngestionStatusError        IngestionStatus = "error"
	IngestionStatusUnauthorized IngestionStatus = "unauthorized"
)

type SourceCapabilities struct {
	CanReadHistory          bool `json:"can_read_history"`
	HasTopics               bool `json:"has_topics"`
	HasDiscussion           bool `json:"has_discussion"`
	SenderMetadataAvailable bool `json:"sender_metadata_available"`
}

type TelegramSource struct {
	ExternalLLMAllowed bool `json:"external_llm_allowed"`

	ID                     uuid.UUID          `json:"id"`
	OwnerID                uuid.UUID          `json:"owner_id"`
	TelegramPeerID         int64              `json:"telegram_peer_id"`
	TelegramAccessHash     int64              `json:"telegram_access_hash,omitempty"`
	Username               string             `json:"username,omitempty"`
	Title                  string             `json:"title"`
	SourceType             TelegramSourceType `json:"source_type"`
	AccessType             AccessType         `json:"access_type"`
	ExpectedType           TelegramSourceType `json:"expected_type,omitempty"`
	DetectedType           TelegramSourceType `json:"detected_type"`
	LinkedDiscussionPeerID *int64             `json:"linked_discussion_peer_id,omitempty"`
	Capabilities           SourceCapabilities `json:"capabilities"`
	IsActive               bool               `json:"is_active"`
	IngestionStatus        IngestionStatus    `json:"ingestion_status"`
	LastMessageID          int64              `json:"last_message_id"`
	LastSyncedAt           *time.Time         `json:"last_synced_at,omitempty"`
	CreatedAt              time.Time          `json:"created_at"`
	UpdatedAt              time.Time          `json:"updated_at"`
}

type ResolveSourceRequest struct {
	Address      string             `json:"address"` // @username, t.me/..., invite link, or numeric id
	ExpectedType TelegramSourceType `json:"expected_type,omitempty"`
}

type ResolveSourceResponse struct {
	Address                string             `json:"address"`
	PeerID                 int64              `json:"peer_id"`
	AccessHash             int64              `json:"access_hash,omitempty"`
	Username               string             `json:"username,omitempty"`
	Title                  string             `json:"title"`
	DetectedType           TelegramSourceType `json:"detected_type"`
	AccessType             AccessType         `json:"access_type"`
	ExpectedType           TelegramSourceType `json:"expected_type,omitempty"`
	TypeMismatch           bool               `json:"type_mismatch"`
	LinkedDiscussionPeerID *int64             `json:"linked_discussion_peer_id,omitempty"`
	Capabilities           SourceCapabilities `json:"capabilities"`
	AccountHasAccess       bool               `json:"account_has_access"`
	CanIngest              bool               `json:"can_ingest"`
}

type CreateTelegramSourceRequest struct {
	ExternalLLMAllowed bool `json:"external_llm_allowed"`

	Address                string              `json:"address"`
	ExpectedType           TelegramSourceType  `json:"expected_type,omitempty"`
	TelegramPeerID         int64               `json:"telegram_peer_id,omitempty"`
	TelegramAccessHash     int64               `json:"telegram_access_hash,omitempty"`
	Username               string              `json:"username,omitempty"`
	Title                  string              `json:"title,omitempty"`
	SourceType             TelegramSourceType  `json:"source_type,omitempty"`
	AccessType             AccessType          `json:"access_type,omitempty"`
	LinkedDiscussionPeerID *int64              `json:"linked_discussion_peer_id,omitempty"`
	Capabilities           *SourceCapabilities `json:"capabilities,omitempty"`
}

type UpdateTelegramSourceStatusRequest struct {
	IngestionStatus IngestionStatus `json:"ingestion_status"`
	IsActive        *bool           `json:"is_active,omitempty"`
}
