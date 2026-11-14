package llm_test

import (
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/llm"
	"testing"
)

func TestInvalidLLMAttributesAreRejected(t *testing.T) {
	cases := []struct {
		name     string
		response llm.LLMExtractionResponse
	}{
		{"invalid currency", llm.LLMExtractionResponse{Classification: domain.ClassificationSeller, Confidence: 0.8, Prices: []llm.LLMExtractedPrice{{Amount: 98, Currency: "USD", Confidence: 0.8}}}},
		{"negative price", llm.LLMExtractionResponse{Classification: domain.ClassificationSeller, Confidence: 0.8, Prices: []llm.LLMExtractedPrice{{Amount: -98, Currency: domain.CurrencyToman, Confidence: 0.8}}}},
		{"invalid condition", llm.LLMExtractionResponse{Classification: domain.ClassificationSeller, Confidence: 0.8, Products: []llm.LLMExtractedProduct{{Name: "iPhone", Condition: "verified", Quantity: 1, Confidence: 0.8}}}},
		{"invalid confidence", llm.LLMExtractionResponse{Classification: domain.ClassificationSeller, Confidence: 8}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := llm.ValidateLLMOutput(&tc.response); err == nil {
				t.Fatalf("invalid attributes silently accepted: %+v", tc.response)
			}
		})
	}
}
