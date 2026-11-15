package matching_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/matching"
)

type failingCatalog struct{}

func (f *failingCatalog) GetAllWithAliases(ctx context.Context) ([]domain.Product, error) {
	return nil, errors.New("catalog unavailable")
}

// TestMatchSellerProduct_CoverageGaps exercises the matcher branches the
// original fixtures never reach: nil extraction, catalog error, variant
// mismatching at every level, the NormalizedName fallback when the product
// name is empty, the keyword level, and the attribute-parsing fallbacks of
// checkVariantMatch (malformed JSON and variant-free attributes).
func TestMatchSellerProduct_CoverageGaps(t *testing.T) {
	ctx := context.Background()

	iphoneID := uuid.New()
	ghostID := uuid.New()
	noattrID := uuid.New()
	badjsonID := uuid.New()
	plainID := uuid.New()

	catalog := &mockCatalog{
		products: []domain.Product{
			{
				ID:             iphoneID,
				Name:           "iPhone 15 Pro",
				NormalizedName: "iphone 15 pro",
				Brand:          "Apple",
				Model:          "15 Pro",
				Category:       "Smartphone",
				Attributes:     []byte(`{"storage": ["256GB", "512GB"]}`),
				Aliases: []domain.ProductAlias{
					{ProductID: iphoneID, Alias: "آیفون ۱۵ پرو", NormalizedAlias: "آیفون ۱۵ پرو", Locale: "fa"},
				},
			},
			{
				ID:             ghostID,
				Name:           "Ghost Device",
				NormalizedName: "ghost device",
				Category:       "Gadget",
			},
			{
				ID:       noattrID,
				Name:     "Sony PlayStation 5 Slim",
				Category: "Console",
			},
			{
				ID:         badjsonID,
				Name:       "Nikon D850",
				Category:   "Camera",
				Attributes: []byte(`not-json`),
			},
			{
				ID:         plainID,
				Name:       "AirPods Max",
				Category:   "Audio",
				Attributes: []byte(`{"color": "silver"}`),
			},
		},
	}
	matcher := matching.NewMatcher(catalog)

	t.Run("nil extracted product is a clean no-match", func(t *testing.T) {
		res, err := matcher.MatchSellerProduct(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Matched {
			t.Fatalf("expected no match for nil product, got %+v", res)
		}
		if res.Reason != "no product extracted" {
			t.Errorf("Reason = %q, want %q", res.Reason, "no product extracted")
		}
	})

	t.Run("catalog error propagates", func(t *testing.T) {
		failMatcher := matching.NewMatcher(&failingCatalog{})
		res, err := failMatcher.MatchSellerProduct(ctx, &domain.ExtractedProduct{Name: "iPhone"})
		if err == nil {
			t.Fatalf("expected catalog error, got result %+v", res)
		}
		if res != nil {
			t.Errorf("res = %+v, want nil on error", res)
		}
	})

	t.Run("exact normalized name match", func(t *testing.T) {
		res, err := matcher.MatchSellerProduct(ctx, &domain.ExtractedProduct{
			Name:    "iPhone 15 Pro",
			Variant: "256GB",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Matched || res.Level != matching.MatchLevelNormalizedName {
			t.Fatalf("result = %+v, want normalized_name match", res)
		}
		if res.ProductID != iphoneID {
			t.Errorf("ProductID = %v, want %v", res.ProductID, iphoneID)
		}
		if res.Confidence != 0.98 {
			t.Errorf("Confidence = %v, want 0.98", res.Confidence)
		}
		if !res.VariantMatched {
			t.Errorf("expected VariantMatched for 256GB in catalog storage list")
		}
	})

	t.Run("NormalizedName fallback when normalized name differs from catalog name", func(t *testing.T) {
		// "ghost device" is normalized to the catalog's NormalizedName, so
		// matching must rely on the NormalizedName comparison rather than an
		// alias or brand/model. (The pNorm == "" fallback in the matcher can
		// only fire for an unnamed catalog product; a non-empty normalized
		// catalog name always satisfies pNorm != "".)
		res, err := matcher.MatchSellerProduct(ctx, &domain.ExtractedProduct{
			Name: "Ghost-Device",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Matched || res.ProductID != ghostID {
			t.Fatalf("result = %+v, want match on ghost product via NormalizedName", res)
		}
		if res.Level != matching.MatchLevelNormalizedName {
			t.Errorf("result = %+v, want normalized_name level", res)
		}
	})

	t.Run("variant conflicts skip every level", func(t *testing.T) {
		// Variant 128GB is absent from the iPhone catalog storage list, so the
		// product is skipped at the name, alias, brand-model and keyword
		// levels alike and matching falls through to the no-match result.
		res, err := matcher.MatchSellerProduct(ctx, &domain.ExtractedProduct{
			Name:    "iPhone",
			Brand:   "Apple",
			Model:   "15 Pro",
			Variant: "128GB",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Matched {
			t.Fatalf("expected no match for unsupported variant, got %+v", res)
		}
		if res.Confidence != 0.0 {
			t.Errorf("Confidence = %v, want 0.0", res.Confidence)
		}
		if res.Reason != "No catalog product matched with sufficient confidence" {
			t.Errorf("Reason = %q", res.Reason)
		}
	})

	t.Run("keyword level match", func(t *testing.T) {
		// The name is not exactly the normalized catalog name, but it contains
		// every keyword of "Sony PlayStation 5 Slim".
		res, err := matcher.MatchSellerProduct(ctx, &domain.ExtractedProduct{
			Name: "Sony PS5 slim console playstation",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Matched || res.Level != matching.MatchLevelKeywords {
			t.Fatalf("result = %+v, want keywords match", res)
		}
		if res.ProductID != noattrID {
			t.Errorf("ProductID = %v, want %v", res.ProductID, noattrID)
		}
		if res.Confidence != 0.82 {
			t.Errorf("Confidence = %v, want 0.82", res.Confidence)
		}
	})

	t.Run("malformed attributes do not block matching", func(t *testing.T) {
		res, err := matcher.MatchSellerProduct(ctx, &domain.ExtractedProduct{
			Name:    "Nikon D850",
			Variant: "256GB",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Unparseable attributes are treated as unconstrained, so the name
		// still matches.
		if !res.Matched || res.ProductID != badjsonID {
			t.Fatalf("result = %+v, want match on bad-json product", res)
		}
		if !res.VariantMatched {
			t.Errorf("expected unconstrained malformed attributes to match any variant")
		}
	})

	t.Run("variant-free attributes are unconstrained", func(t *testing.T) {
		res, err := matcher.MatchSellerProduct(ctx, &domain.ExtractedProduct{
			Name:    "AirPods Max",
			Variant: "256GB",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Matched || res.ProductID != plainID {
			t.Fatalf("result = %+v, want match on plain-attributes product", res)
		}
		if !res.VariantMatched {
			t.Errorf("expected variant-free attributes to match any variant")
		}
	})
}
