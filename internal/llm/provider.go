package llm

import (
	"context"

	"github.com/perso/perso-mind/internal/domain"
)

type LLMExtractionResponse struct {
	Classification domain.Classification `json:"classification"` // seller, buyer, neither, uncertain
	Confidence     float64               `json:"confidence"`
	Products       []LLMExtractedProduct `json:"products"`
	Prices         []LLMExtractedPrice   `json:"prices"`
	Location       *LLMExtractedLocation `json:"location,omitempty"`
	MissingFields  []domain.MissingField `json:"missing_fields"`
	RawAnalysis    string                `json:"raw_analysis,omitempty"`
	TokenUsage     TokenUsage            `json:"token_usage"`
}

type LLMExtractedProduct struct {
	Name         string               `json:"name"`
	Brand        string               `json:"brand,omitempty"`
	Manufacturer string               `json:"manufacturer,omitempty"`
	Model        string               `json:"model,omitempty"`
	Variant      string               `json:"variant,omitempty"`
	Condition    domain.ItemCondition `json:"condition"`
	Quantity     int                  `json:"quantity"`
	Confidence   float64              `json:"confidence"`
	EvidenceSpan string               `json:"evidence_span"`
}

type LLMExtractedPrice struct {
	Amount       int64           `json:"amount"`
	Currency     domain.Currency `json:"currency"`
	Raw          string          `json:"raw"`
	Negotiable   bool            `json:"negotiable"`
	Confidence   float64         `json:"confidence"`
	EvidenceSpan string          `json:"evidence_span"`
}

type LLMExtractedLocation struct {
	City         string  `json:"city,omitempty"`
	Area         string  `json:"area,omitempty"` // Tehran district or area
	LocationText string  `json:"location_text,omitempty"`
	Confidence   float64 `json:"confidence"`
	EvidenceSpan string  `json:"evidence_span"`
}

type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Provider interface {
	ExtractMarketplaceInfo(ctx context.Context, messageText string) (*LLMExtractionResponse, error)
	GenerateConversationQuestion(ctx context.Context, productName string, targetField domain.MissingField, history []domain.ConversationMessage) (string, error)
	ValidateConsentResponse(ctx context.Context, sellerReply string) (bool, error)
	Name() string
}
