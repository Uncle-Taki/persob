package worker_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/extraction"
	"github.com/perso/perso-mind/internal/matching"
)

type mockCatalog struct {
	products []domain.Product
}

func (m *mockCatalog) GetAllWithAliases(ctx context.Context) ([]domain.Product, error) {
	return m.products, nil
}

func TestPipelineExtractionAndMatching(t *testing.T) {
	// 1. Simulate an incoming Persian Telegram seller message
	rawMsg := &domain.TelegramMessage{
		TelegramChatID:    -1001234567890,
		TelegramMessageID: 101,
		SenderID:          987654321,
		SenderUsername:    "apple_store_tehran",
		SenderDisplayName: "فروشگاه اپل علاءالدین",
		MessageText:       "فروش فوری آیفون ۱۵ پرو ۲۵۶ گیگ در حد نو رنگ تیتانیوم، قیمت ۹۸ میلیون تومان. تحویل پاساژ علاءالدین طبقه ۲",
		MessageDate:       time.Now(),
		ReceivedAt:        time.Now(),
	}

	// 2. Run deterministic extraction
	extractor := extraction.NewExtractor()
	extracted := extractor.ExtractFromMessage(rawMsg)

	if extracted.Classification != domain.ClassificationSeller {
		t.Fatalf("Classification = %v, want seller", extracted.Classification)
	}

	if len(extracted.ExtractedProducts) == 0 {
		t.Fatalf("Expected extracted products")
	}

	prod := extracted.ExtractedProducts[0]
	if prod.Brand != "Apple" || prod.Model != "15 Pro" {
		t.Errorf("Product = %s %s, want Apple 15 Pro", prod.Brand, prod.Model)
	}
	if prod.Variant != "256GB" {
		t.Errorf("Variant = %s, want 256GB", prod.Variant)
	}

	if len(extracted.ExtractedPrices) == 0 || extracted.ExtractedPrices[0].Amount != 98_000_000 {
		t.Errorf("Price extraction failed, got: %v", extracted.ExtractedPrices)
	}

	// 3. Match against product catalog
	prodID := uuid.New()
	catalog := &mockCatalog{
		products: []domain.Product{
			{
				ID:             prodID,
				Name:           "iPhone 15 Pro",
				NormalizedName: "iphone 15 pro",
				Brand:          "Apple",
				Model:          "15 Pro",
				Category:       "Smartphone",
				Attributes:     []byte(`{"storage": ["128GB", "256GB", "512GB"]}`),
			},
		},
	}

	matcher := matching.NewMatcher(catalog)
	matchRes, err := matcher.MatchSellerProduct(context.Background(), &prod)
	if err != nil {
		t.Fatalf("match error: %v", err)
	}

	if !matchRes.Matched {
		t.Fatalf("expected product to match catalog")
	}
	if matchRes.ProductID != prodID {
		t.Errorf("Matched ID = %v, want %v", matchRes.ProductID, prodID)
	}
	if !matchRes.VariantMatched {
		t.Errorf("Expected variant 256GB to match")
	}
}
