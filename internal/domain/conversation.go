package domain

import (
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ConversationState string

const (
	ConvStateNew              ConversationState = "NEW"
	ConvStateAwaitingConsent  ConversationState = "AWAITING_CONSENT"
	ConvStateAwaitingApproval ConversationState = "AWAITING_APPROVAL"
	ConvStateActive           ConversationState = "ACTIVE"
	ConvStateWaitingForReply  ConversationState = "WAITING_FOR_REPLY"
	ConvStateCompleted        ConversationState = "COMPLETED"
	ConvStateDeclined         ConversationState = "DECLINED"
	ConvStateOptedOut         ConversationState = "OPTED_OUT"
	ConvStatePaused           ConversationState = "PAUSED"
)

type MissingField string

const (
	FieldPrice        MissingField = "price"
	FieldBranded      MissingField = "branded"
	FieldManufacturer MissingField = "manufacturer"
	FieldExpiration   MissingField = "expiration_date"
	FieldCity         MissingField = "seller_city"
	FieldTehranArea   MissingField = "seller_tehran_area"
)

type Conversation struct {
	ID                  uuid.UUID         `json:"id"`
	OfferID             *uuid.UUID        `json:"offer_id,omitempty"`
	SellerID            int64             `json:"seller_id"`
	ProductID           *uuid.UUID        `json:"product_id,omitempty"`
	State               ConversationState `json:"state"`
	ConsentGranted      bool              `json:"consent_granted"`
	ApprovedByUserID    *uuid.UUID        `json:"approved_by_user_id,omitempty"`
	ApprovedAt          *time.Time        `json:"approved_at,omitempty"`
	QuestionsAskedCount int               `json:"questions_asked_count"`
	MaxQuestions        int               `json:"max_questions"`
	MissingFields       []MissingField    `json:"missing_fields"`
	CollectedData       map[string]any    `json:"collected_data"`
	Notes               string            `json:"notes,omitempty"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`

	// Relational
	Offer   *Offer   `json:"offer,omitempty"`
	Product *Product `json:"product,omitempty"`
}

type ConversationMessageSender string

const (
	SenderAssistant ConversationMessageSender = "assistant"
	SenderSeller    ConversationMessageSender = "seller"
)

type ConversationMessage struct {
	ID             primitive.ObjectID        `bson:"_id,omitempty" json:"id"`
	ConversationID string                    `bson:"conversation_id" json:"conversation_id"`
	Sender         ConversationMessageSender `bson:"sender" json:"sender"`
	Text           string                    `bson:"text" json:"text"`
	SentAt         time.Time                 `bson:"sent_at" json:"sent_at"`
	Delivered      bool                      `bson:"delivered" json:"delivered"`
}

type OutreachDraft struct {
	ConversationID uuid.UUID    `json:"conversation_id"`
	SellerID       int64        `json:"seller_id"`
	ProposedPrompt string       `json:"proposed_prompt"`
	TargetField    MissingField `json:"target_field"`
	CreatedBy      string       `json:"created_by"`
	CreatedAt      time.Time    `json:"created_at"`
}
