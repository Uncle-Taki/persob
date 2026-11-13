package matching_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/matching"
)

type mockCatalog struct {
	products []domain.Product
}

func (m *mockCatalog) GetAllWithAliases(ctx context.Context) ([]domain.Product, error) {
	return m.products, nil
}

func TestMatcher(t *testing.T) {
	iphoneID := uuid.New()
	s24ID := uuid.New()

	catalog := &mockCatalog{
		products: []domain.Product{
			{
				ID:             iphoneID,
				Name:           "iPhone 15 Pro",
				NormalizedName: "iphone 15 pro",
				Brand:          "Apple",
				Model:          "15 Pro",
				Category:       "Smartphone",
				Attributes:     []byte(`{"storage": ["128GB", "256GB", "512GB"]}`),
				Aliases: []domain.ProductAlias{
					{
						ProductID:       iphoneID,
						Alias:           "آیفون ۱۵ پرو",
						NormalizedAlias: "آیفون ۱۵ پرو",
						Locale:          "fa",
					},
				},
			},
			{
				ID:             s24ID,
				Name:           "Samsung Galaxy S24 Ultra",
				NormalizedName: "samsung galaxy s24 ultra",
				Brand:          "Samsung",
				Model:          "S24 Ultra",
				Category:       "Smartphone",
				Attributes:     []byte(`{"storage": ["256GB", "512GB"]}`),
				Aliases: []domain.ProductAlias{
					{
						ProductID:       s24ID,
						Alias:           "اس ۲۴ اولترا",
						NormalizedAlias: "اس ۲۴ اولترا",
						Locale:          "fa",
					},
				},
			},
		},
	}

	matcher := matching.NewMatcher(catalog)
	ctx := context.Background()

	// 1. Alias Match Test
	t.Run("Match via Persian Alias", func(t *testing.T) {
		extracted := &domain.ExtractedProduct{
			Name:       "آیفون ۱۵ پرو",
			Brand:      "Apple",
			Model:      "15 Pro",
			Variant:    "256GB",
			Condition:  domain.ConditionLikeNew,
			Confidence: 0.90,
		}

		res, err := matcher.MatchSellerProduct(ctx, extracted)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Matched {
			t.Fatalf("expected match, got unmatched")
		}
		if res.ProductID != iphoneID {
			t.Errorf("matched product ID = %v, want %v", res.ProductID, iphoneID)
		}
		if !res.VariantMatched {
			t.Errorf("expected variant to match (256GB in catalog attributes)")
		}
	})

	// 2. Brand and Model Match Test
	t.Run("Match via Brand and Model", func(t *testing.T) {
		extracted := &domain.ExtractedProduct{
			Name:       "Galaxy S24 Ultra",
			Brand:      "Samsung",
			Model:      "S24 Ultra",
			Variant:    "512GB",
			Condition:  domain.ConditionNew,
			Confidence: 0.88,
		}

		res, err := matcher.MatchSellerProduct(ctx, extracted)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Matched {
			t.Fatalf("expected match, got unmatched")
		}
		if res.ProductID != s24ID {
			t.Errorf("matched product ID = %v, want %v", res.ProductID, s24ID)
		}
	})

	// 3. Unmatched product
	t.Run("Unmatched unknown product", func(t *testing.T) {
		extracted := &domain.ExtractedProduct{
			Name:  "Nokia 3310 Classic",
			Brand: "Nokia",
			Model: "3310",
		}

		res, err := matcher.MatchSellerProduct(ctx, extracted)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Matched {
			t.Errorf("expected no match, but matched: %v", res.ProductName)
		}
	})
}
