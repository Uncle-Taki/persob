package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Classification string

const (
	ClassificationSeller     Classification = "seller"
	ClassificationBuyer      Classification = "buyer"
	ClassificationNeither    Classification = "neither"
	ClassificationIrrelevant Classification = "irrelevant"
	ClassificationUncertain  Classification = "uncertain"
)

type ProcessingStatus string

const (
	ProcessingStatusPending    ProcessingStatus = "pending"
	ProcessingStatusProcessing ProcessingStatus = "processing"
	ProcessingStatusProcessed  ProcessingStatus = "processed"
	ProcessingStatusFailed     ProcessingStatus = "failed"
	ProcessingStatusIgnored    ProcessingStatus = "ignored"
)

type TelegramMessage struct {
	Deleted bool `bson:"deleted" json:"deleted"`

	OriginalMessageText string                 `bson:"original_message_text,omitempty" json:"original_message_text,omitempty"`
	ID                  primitive.ObjectID     `bson:"_id,omitempty" json:"id"`
	SourceID            string                 `bson:"source_id,omitempty" json:"source_id,omitempty"`
	TelegramMessageID   int64                  `bson:"telegram_message_id" json:"telegram_message_id"`
	TelegramChatID      int64                  `bson:"telegram_chat_id" json:"telegram_chat_id"`
	TelegramPeerID      int64                  `bson:"telegram_peer_id,omitempty" json:"telegram_peer_id,omitempty"`
	TopicID             *int64                 `bson:"topic_id,omitempty" json:"topic_id,omitempty"`
	LinkedPostID        *int64                 `bson:"linked_post_id,omitempty" json:"linked_post_id,omitempty"`
	ChatTitle           string                 `bson:"chat_title,omitempty" json:"chat_title,omitempty"`
	ChatUsername        string                 `bson:"chat_username,omitempty" json:"chat_username,omitempty"`
	ChatType            string                 `bson:"chat_type,omitempty" json:"chat_type,omitempty"`
	SourceType          string                 `bson:"source_type,omitempty" json:"source_type,omitempty"`
	SenderID            int64                  `bson:"sender_id" json:"sender_id"`
	SenderType          string                 `bson:"sender_type,omitempty" json:"sender_type,omitempty"`
	SenderUsername      string                 `bson:"sender_username,omitempty" json:"sender_username,omitempty"`
	SenderDisplayName   string                 `bson:"sender_display_name,omitempty" json:"sender_display_name,omitempty"`
	MessageText         string                 `bson:"message_text" json:"message_text"`
	MessageEntities     []interface{}          `bson:"message_entities,omitempty" json:"message_entities,omitempty"`
	MediaMetadata       map[string]interface{} `bson:"media_metadata,omitempty" json:"media_metadata,omitempty"`
	ReplyToMessageID    *int64                 `bson:"reply_to_message_id,omitempty" json:"reply_to_message_id,omitempty"`
	MessageDate         time.Time              `bson:"message_date" json:"message_date"`
	EditedDate          *time.Time             `bson:"edited_date,omitempty" json:"edited_date,omitempty"`
	ReceivedAt          time.Time              `bson:"received_at" json:"received_at"`
	Language            string                 `bson:"language,omitempty" json:"language,omitempty"`
	NormalizedText      string                 `bson:"normalized_text,omitempty" json:"normalized_text,omitempty"`
	ProcessingStatus    ProcessingStatus       `bson:"processing_status" json:"processing_status"`
	ProcessingVersion   int                    `bson:"processing_version" json:"processing_version"`
}

type TelegramUpdate struct {
	ID         primitive.ObjectID     `bson:"_id,omitempty" json:"id"`
	UpdateID   int64                  `bson:"update_id" json:"update_id"`
	EventType  string                 `bson:"event_type" json:"event_type"`
	ChatID     int64                  `bson:"chat_id,omitempty" json:"chat_id,omitempty"`
	Payload    map[string]interface{} `bson:"payload" json:"payload"`
	ReceivedAt time.Time              `bson:"received_at" json:"received_at"`
}

type ExtractedProduct struct {
	Name         string        `bson:"name" json:"name"`
	Brand        string        `bson:"brand,omitempty" json:"brand,omitempty"`
	Manufacturer string        `bson:"manufacturer,omitempty" json:"manufacturer,omitempty"`
	Model        string        `bson:"model,omitempty" json:"model,omitempty"`
	Variant      string        `bson:"variant,omitempty" json:"variant,omitempty"`
	DosageForm   string        `bson:"dosage_form,omitempty" json:"dosage_form,omitempty"`
	Strength     string        `bson:"strength,omitempty" json:"strength,omitempty"`
	PackSize     int           `bson:"pack_size,omitempty" json:"pack_size,omitempty"`
	Unit         string        `bson:"unit,omitempty" json:"unit,omitempty"`
	Condition    ItemCondition `bson:"condition" json:"condition"`
	Quantity     int           `bson:"quantity" json:"quantity"`
	EvidenceSpan string        `bson:"evidence_span,omitempty" json:"evidence_span,omitempty"`
	Confidence   float64       `bson:"confidence" json:"confidence"`
}

type ExtractedPrice struct {
	Amount       int64    `bson:"amount" json:"amount"`
	Currency     Currency `bson:"currency" json:"currency"`
	Raw          string   `bson:"raw" json:"raw"`
	Negotiable   bool     `bson:"negotiable" json:"negotiable"`
	Confidence   float64  `bson:"confidence" json:"confidence"`
	EvidenceSpan string   `bson:"evidence_span,omitempty" json:"evidence_span,omitempty"`
}

type ExtractedLocation struct {
	City         string `bson:"city,omitempty" json:"city,omitempty"`
	Area         string `bson:"area,omitempty" json:"area,omitempty"` // Tehran district/area
	LocationText string `bson:"location_text,omitempty" json:"location_text,omitempty"`
	EvidenceSpan string `bson:"evidence_span,omitempty" json:"evidence_span,omitempty"`
}

type ExtractedMessage struct {
	SourceRevisionAt time.Time `bson:"source_revision_at" json:"source_revision_at"`

	ID                primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	MessageID         string             `bson:"message_id" json:"message_id"` // format: {chat_id}_{message_id}
	Classification    Classification     `bson:"classification" json:"classification"`
	ExtractedProducts []ExtractedProduct `bson:"extracted_products" json:"extracted_products"`
	ExtractedPrices   []ExtractedPrice   `bson:"extracted_prices" json:"extracted_prices"`
	SellerMetadata    map[string]string  `bson:"seller_metadata,omitempty" json:"seller_metadata,omitempty"`
	Location          *ExtractedLocation `bson:"location,omitempty" json:"location,omitempty"`
	BrandedKnown      bool               `bson:"branded_known" json:"branded_known"`
	Branded           bool               `bson:"branded" json:"branded"`
	Confidence        float64            `bson:"confidence" json:"confidence"`
	ExtractionVersion int                `bson:"extraction_version" json:"extraction_version"`
	CreatedAt         time.Time          `bson:"created_at" json:"created_at"`
}

type ProcessingJob struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	JobID      string             `bson:"job_id" json:"job_id"`
	Type       string             `bson:"type" json:"type"`
	TargetID   string             `bson:"target_id" json:"target_id"`
	Status     string             `bson:"status" json:"status"`
	RetryCount int                `bson:"retry_count" json:"retry_count"`
	LastError  string             `bson:"last_error,omitempty" json:"last_error,omitempty"`
	CreatedAt  time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt  time.Time          `bson:"updated_at" json:"updated_at"`
}

type LLMExtraction struct {
	ID                primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	MessageID         string             `bson:"message_id" json:"message_id"`
	RawPrompt         string             `bson:"raw_prompt,omitempty" json:"raw_prompt,omitempty"`
	RawResponse       string             `bson:"raw_response,omitempty" json:"raw_response,omitempty"`
	ExtractedProducts []ExtractedProduct `bson:"extracted_products" json:"extracted_products"`
	ExtractedPrices   []ExtractedPrice   `bson:"extracted_prices" json:"extracted_prices"`
	ModelName         string             `bson:"model_name,omitempty" json:"model_name,omitempty"`
	CreatedAt         time.Time          `bson:"created_at" json:"created_at"`
}

type ConversationEvent struct {
	ID             primitive.ObjectID     `bson:"_id,omitempty" json:"id"`
	ConversationID string                 `bson:"conversation_id" json:"conversation_id"`
	EventType      string                 `bson:"event_type" json:"event_type"`
	Payload        map[string]interface{} `bson:"payload" json:"payload"`
	Timestamp      time.Time              `bson:"timestamp" json:"timestamp"`
}
