package llm

import (
	"math"
	"testing"

	"github.com/perso/perso-mind/internal/domain"
)

func TestCleanString(t *testing.T) {
	// Surrounding spaces trimmed; invalid UTF-8 bytes dropped (NUL is valid
	// UTF-8, so ToValidUTF8 keeps it).
	if got := cleanString("  bad bytes  "); got != "bad bytes" {
		t.Errorf("cleanString = %q, want %q", got, "bad bytes")
	}
	if got := cleanString("\xffhello\xfe"); got != "hello" {
		t.Errorf("cleanString(invalid bytes) = %q, want %q", got, "hello")
	}
	if got := cleanString("\x00nul\x00"); got != "\x00nul\x00" {
		t.Errorf("cleanString(nul) = %q, want %q", got, "\x00nul\x00")
	}
	if got := cleanString("   "); got != "" {
		t.Errorf("cleanString(spaces) = %q, want empty", got)
	}
}

func TestValidateLLMOutputFullMatrix(t *testing.T) {
	t.Run("nil response", func(t *testing.T) {
		if err := ValidateLLMOutput(nil); err == nil || err.Error() != "nil llm extraction response" {
			t.Fatalf("err = %v, want nil response error", err)
		}
	})

	t.Run("invalid classification", func(t *testing.T) {
		resp := &LLMExtractionResponse{Classification: "hacker", Confidence: 0.5}
		if err := ValidateLLMOutput(resp); err == nil || err.Error() != "invalid classification" {
			t.Fatalf("err = %v, want invalid classification", err)
		}
	})

	t.Run("product attributes rejected", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			product LLMExtractedProduct
			wantErr string
		}{
			{"blank name", LLMExtractedProduct{Name: "  ", Condition: domain.ConditionUsedGood, Quantity: 1, Confidence: 0.9}, "invalid product attributes"},
			{"zero quantity", LLMExtractedProduct{Name: "iPhone", Condition: domain.ConditionUsedGood, Quantity: 0, Confidence: 0.9}, "invalid product attributes"},
			{"bad confidence", LLMExtractedProduct{Name: "iPhone", Condition: domain.ConditionUsedGood, Quantity: 1, Confidence: 1.5}, "invalid product attributes"},
		} {
			resp := &LLMExtractionResponse{Classification: domain.ClassificationSeller, Confidence: 0.9, Products: []LLMExtractedProduct{tc.product}}
			if err := ValidateLLMOutput(resp); err == nil || err.Error() != tc.wantErr {
				t.Fatalf("%s: err = %v, want %q", tc.name, err, tc.wantErr)
			}
		}
	})

	t.Run("product strings cleaned in place", func(t *testing.T) {
		resp := &LLMExtractionResponse{
			Classification: domain.ClassificationSeller,
			Confidence:     0.9,
			Products: []LLMExtractedProduct{
				{Name: "  iPhone 15  ", Brand: " \xffApple ", Model: " 15 ", Condition: domain.ConditionUsedGood, Quantity: 1, Confidence: 0.9, EvidenceSpan: "x"},
			},
		}
		if err := ValidateLLMOutput(resp); err != nil {
			t.Fatalf("err = %v", err)
		}
		if resp.Products[0].Name != "iPhone 15" || resp.Products[0].Brand != "Apple" || resp.Products[0].Model != "15" {
			t.Errorf("product not cleaned: %+v", resp.Products[0])
		}
	})

	t.Run("price confidence rejected", func(t *testing.T) {
		resp := &LLMExtractionResponse{Classification: domain.ClassificationSeller, Confidence: 0.9, Prices: []LLMExtractedPrice{{Amount: 100, Currency: domain.CurrencyToman, Confidence: 2}}}
		if err := ValidateLLMOutput(resp); err == nil || err.Error() != "invalid price attributes" {
			t.Fatalf("err = %v, want invalid price attributes", err)
		}
	})

	t.Run("valid rial price and valid fields accepted", func(t *testing.T) {
		resp := &LLMExtractionResponse{
			Classification: domain.ClassificationSeller,
			Confidence:     0.9,
			Prices:         []LLMExtractedPrice{{Amount: 980_000_000, Currency: domain.CurrencyRial, Confidence: 0.9, EvidenceSpan: "x"}},
			MissingFields:  []domain.MissingField{domain.FieldPrice, domain.FieldBranded, domain.FieldManufacturer, domain.FieldExpiration, domain.FieldCity, domain.FieldTehranArea},
		}
		if err := ValidateLLMOutput(resp); err != nil {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("location confidence rejected", func(t *testing.T) {
		resp := &LLMExtractionResponse{
			Classification: domain.ClassificationSeller,
			Confidence:     0.9,
			Location:       &LLMExtractedLocation{City: "تهران", Confidence: 1.2},
		}
		if err := ValidateLLMOutput(resp); err == nil || err.Error() != "invalid location confidence" {
			t.Fatalf("err = %v, want invalid location confidence", err)
		}
	})

	t.Run("valid location accepted", func(t *testing.T) {
		resp := &LLMExtractionResponse{
			Classification: domain.ClassificationSeller,
			Confidence:     0.9,
			Location:       &LLMExtractedLocation{City: "تهران", Confidence: 0.7, EvidenceSpan: "x"},
		}
		if err := ValidateLLMOutput(resp); err != nil {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("unsupported missing field", func(t *testing.T) {
		resp := &LLMExtractionResponse{
			Classification: domain.ClassificationSeller,
			Confidence:     0.9,
			MissingFields:  []domain.MissingField{domain.FieldCondition},
		}
		if err := ValidateLLMOutput(resp); err == nil || err.Error() != "unsupported missing field" {
			t.Fatalf("err = %v, want unsupported missing field", err)
		}
	})
}

func TestValidateLLMEvidenceFullMatrix(t *testing.T) {
	original := "فروش آیفون ۱۵ پرو ۲۵۶ تهران"

	valid := func() *LLMExtractionResponse {
		return &LLMExtractionResponse{
			Classification: domain.ClassificationSeller,
			Confidence:     0.9,
			Products:       []LLMExtractedProduct{{Name: "iPhone", Condition: domain.ConditionUsedGood, Quantity: 1, Confidence: 0.9, EvidenceSpan: "آیفون"}},
			Prices:         []LLMExtractedPrice{{Amount: 1, Currency: domain.CurrencyToman, Confidence: 0.9, EvidenceSpan: "۲۵۶"}},
			Location:       &LLMExtractedLocation{City: "تهران", Confidence: 0.8, EvidenceSpan: "تهران"},
		}
	}

	t.Run("all evidence found in original", func(t *testing.T) {
		if err := ValidateLLMEvidence(valid(), original); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
	})

	t.Run("nil response propagates output error", func(t *testing.T) {
		if err := ValidateLLMEvidence(nil, original); err == nil || err.Error() != "nil llm extraction response" {
			t.Fatalf("err = %v, want nil response error", err)
		}
	})

	t.Run("invalid output propagates output error", func(t *testing.T) {
		resp := valid()
		resp.Classification = "hacker"
		if err := ValidateLLMEvidence(resp, original); err == nil || err.Error() != "invalid classification" {
			t.Fatalf("err = %v, want invalid classification", err)
		}
	})

	t.Run("blank product span rejected", func(t *testing.T) {
		resp := valid()
		resp.Products[0].EvidenceSpan = "   "
		if err := ValidateLLMEvidence(resp, original); err == nil || err.Error() != "unsupported product evidence" {
			t.Fatalf("err = %v, want unsupported product evidence", err)
		}
	})

	t.Run("fabricated product span rejected", func(t *testing.T) {
		resp := valid()
		resp.Products[0].EvidenceSpan = "ساختگی"
		if err := ValidateLLMEvidence(resp, original); err == nil || err.Error() != "unsupported product evidence" {
			t.Fatalf("err = %v, want unsupported product evidence", err)
		}
	})

	t.Run("fabricated price span rejected", func(t *testing.T) {
		resp := valid()
		resp.Prices[0].EvidenceSpan = "ساختگی"
		if err := ValidateLLMEvidence(resp, original); err == nil || err.Error() != "unsupported price evidence" {
			t.Fatalf("err = %v, want unsupported price evidence", err)
		}
	})

	t.Run("fabricated location span rejected", func(t *testing.T) {
		resp := valid()
		resp.Location.EvidenceSpan = "ساختگی"
		if err := ValidateLLMEvidence(resp, original); err == nil || err.Error() != "unsupported location evidence" {
			t.Fatalf("err = %v, want unsupported location evidence", err)
		}
	})
}

func TestMergeDeterministicAndLLMFallbacks(t *testing.T) {
	t.Run("nil llm response returns deterministic", func(t *testing.T) {
		det := &domain.ExtractedMessage{Classification: domain.ClassificationSeller}
		if got := MergeDeterministicAndLLM(det, nil); got != det {
			t.Errorf("expected same pointer, got %p want %p", got, det)
		}
	})

	t.Run("llm prices and products enriched when deterministic empty", func(t *testing.T) {
		det := &domain.ExtractedMessage{Classification: domain.ClassificationUncertain, Confidence: 0.4}
		llmResp := &LLMExtractionResponse{
			Classification: domain.ClassificationBuyer,
			Confidence:     0.8,
			Prices: []LLMExtractedPrice{
				{Amount: 5_000_000, Currency: domain.CurrencyRial, Raw: "پنج میلیون", Negotiable: true, Confidence: 1.0, EvidenceSpan: "x"},
			},
			Products: []LLMExtractedProduct{
				{Name: "آیفون", Brand: "اپل", Manufacturer: "اپل", Model: "15", Variant: "256GB", Condition: domain.ConditionLikeNew, Quantity: 2, Confidence: 0.7, EvidenceSpan: "x"},
			},
		}
		merged := MergeDeterministicAndLLM(det, llmResp)

		if len(merged.ExtractedPrices) != 1 || merged.ExtractedPrices[0].Amount != 5_000_000 ||
			merged.ExtractedPrices[0].Currency != domain.CurrencyRial || !merged.ExtractedPrices[0].Negotiable {
			t.Errorf("prices not enriched: %+v", merged.ExtractedPrices)
		}
		// Unverified LLM prices must be penalized: 1.0 * 0.9.
		if merged.ExtractedPrices[0].Confidence != 0.9 {
			t.Errorf("llm price confidence = %v, want 0.9 penalty", merged.ExtractedPrices[0].Confidence)
		}
		if len(merged.ExtractedProducts) != 1 || merged.ExtractedProducts[0].Name != "آیفون" ||
			merged.ExtractedProducts[0].Quantity != 2 || merged.ExtractedProducts[0].Condition != domain.ConditionLikeNew {
			t.Errorf("products not enriched: %+v", merged.ExtractedProducts)
		}
	})

	t.Run("uncertain classification replaced and confidence averaged", func(t *testing.T) {
		det := &domain.ExtractedMessage{Classification: domain.ClassificationUncertain, Confidence: 0.4}
		llmResp := &LLMExtractionResponse{Classification: domain.ClassificationBuyer, Confidence: 0.8}
		merged := MergeDeterministicAndLLM(det, llmResp)
		if merged.Classification != domain.ClassificationBuyer {
			t.Errorf("Classification = %v, want buyer", merged.Classification)
		}
		if math.Abs(merged.Confidence-0.6) > 1e-9 {
			t.Errorf("Confidence = %v, want average 0.6", merged.Confidence)
		}
	})

	t.Run("deterministic values preserved over llm", func(t *testing.T) {
		det := &domain.ExtractedMessage{
			Classification:    domain.ClassificationSeller,
			Confidence:        0.9,
			ExtractedPrices:   []domain.ExtractedPrice{{Amount: 1, Currency: domain.CurrencyToman, Confidence: 0.9}},
			ExtractedProducts: []domain.ExtractedProduct{{Name: "det-product"}},
			Location:          &domain.ExtractedLocation{City: "det-city"},
		}
		llmResp := &LLMExtractionResponse{
			Classification: domain.ClassificationBuyer,
			Confidence:     0.5,
			Prices:         []LLMExtractedPrice{{Amount: 99, Currency: domain.CurrencyToman, Confidence: 0.5}},
			Products:       []LLMExtractedProduct{{Name: "llm-product"}},
			Location:       &LLMExtractedLocation{City: "llm-city"},
		}
		merged := MergeDeterministicAndLLM(det, llmResp)
		if merged.Classification != domain.ClassificationSeller || merged.Confidence != 0.9 {
			t.Errorf("classification/confidence overwritten: %v %v", merged.Classification, merged.Confidence)
		}
		if merged.ExtractedPrices[0].Amount != 1 || merged.ExtractedProducts[0].Name != "det-product" || merged.Location.City != "det-city" {
			t.Errorf("deterministic values overwritten: %+v", merged)
		}
	})

	t.Run("llm uncertain classification does not replace", func(t *testing.T) {
		det := &domain.ExtractedMessage{Classification: domain.ClassificationUncertain, Confidence: 0.4}
		llmResp := &LLMExtractionResponse{Classification: domain.ClassificationUncertain, Confidence: 0.8}
		merged := MergeDeterministicAndLLM(det, llmResp)
		if merged.Classification != domain.ClassificationUncertain || merged.Confidence != 0.4 {
			t.Errorf("uncertain llm response must not change result: %v %v", merged.Classification, merged.Confidence)
		}
	})
}
