package llm

import (
	"context"

	"github.com/perso/perso-mind/internal/domain"
)

type MockProvider struct {
	SimulateError  bool
	CustomResponse *LLMExtractionResponse
}

func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (m *MockProvider) Name() string {
	return "mock-llm-provider"
}

func (m *MockProvider) ExtractMarketplaceInfo(ctx context.Context, messageText string) (*LLMExtractionResponse, error) {
	if m.SimulateError {
		return nil, context.DeadlineExceeded
	}

	if m.CustomResponse != nil {
		return m.CustomResponse, nil
	}

	// Default simulated extraction
	return &LLMExtractionResponse{
		Classification: domain.ClassificationSeller,
		Confidence:     0.90,
		Products: []LLMExtractedProduct{
			{
				Name:         "iPhone 15 Pro",
				Brand:        "Apple",
				Manufacturer: "Apple",
				Model:        "15 Pro",
				Variant:      "256GB",
				Condition:    domain.ConditionUsedGood,
				Quantity:     1,
				Confidence:   0.92,
				EvidenceSpan: "آیفون ۱۵ پرو ۲۵۶",
			},
		},
		Prices: []LLMExtractedPrice{
			{
				Amount:       98000000,
				Currency:     domain.CurrencyToman,
				Raw:          "۹۸ میلیون تومان",
				Negotiable:   false,
				Confidence:   0.95,
				EvidenceSpan: "۹۸ میلیون تومان",
			},
		},
		Location: &LLMExtractedLocation{
			City:         "تهران",
			Area:         "علاءالدین",
			LocationText: "علاءالدین",
			Confidence:   0.88,
			EvidenceSpan: "علاءالدین",
		},
		MissingFields: []domain.MissingField{domain.FieldExpiration},
		TokenUsage: TokenUsage{
			PromptTokens:     85,
			CompletionTokens: 60,
			TotalTokens:      145,
		},
	}, nil
}

func (m *MockProvider) GenerateConversationQuestion(
	ctx context.Context,
	productName string,
	targetField domain.MissingField,
	history []domain.ConversationMessage,
) (string, error) {
	return fallbackQuestion(targetField, productName), nil
}

func (m *MockProvider) ValidateConsentResponse(ctx context.Context, sellerReply string) (bool, error) {
	consent, _ := deterministicConsent(sellerReply)
	return consent, nil
}
