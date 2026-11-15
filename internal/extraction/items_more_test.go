package extraction_test

import (
	"testing"

	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/extraction"
)

// TestListingItems_CoverageGaps covers the two ListingItems branches the
// original fixtures never reach: the single-product/single-price fast path
// (items.go:33-35) and the duplicate-key dedup continue (items.go:27).
func TestListingItems_CoverageGaps(t *testing.T) {
	t.Run("single product with exactly one price is associated", func(t *testing.T) {
		msg := &domain.TelegramMessage{MessageText: "فروش آیفون ۱۵ پرو ۲۵۶ گیگ، قیمت ۹۸ میلیون تومان"}
		ext := extraction.NewExtractor().ExtractFromMessage(msg)

		items := extraction.ListingItems(msg, ext)
		if len(items) != 1 {
			t.Fatalf("items = %+v, want exactly 1", items)
		}
		if items[0].Product.Name != "iPhone 15 Pro" || items[0].Product.Variant != "256GB" {
			t.Errorf("product = %+v, want iPhone 15 Pro 256GB", items[0].Product)
		}
		if items[0].Price == nil || items[0].Price.Amount != 98_000_000 {
			t.Fatalf("price = %+v, want the single 98M price", items[0].Price)
		}
	})

	t.Run("duplicate product identities are deduplicated by key", func(t *testing.T) {
		// ListingItems keys items on brand:model:variant:name, so a hand-built
		// extraction with two identical products must collapse to one item.
		p := domain.ExtractedProduct{
			Name:       "iPhone 15 Pro",
			Brand:      "Apple",
			Model:      "15 Pro",
			Variant:    "256GB",
			Quantity:   1,
			Confidence: 0.85,
		}
		ext := &domain.ExtractedMessage{
			ExtractedProducts: []domain.ExtractedProduct{p, p},
		}
		msg := &domain.TelegramMessage{MessageText: "آیفون ۱۵ پرو ۲۵۶ گیگ قیمت ۹۸ میلیون تومان"}

		items := extraction.ListingItems(msg, ext)
		if len(items) != 1 {
			t.Fatalf("items = %+v, want exactly 1 after key dedup", items)
		}
		if items[0].Key == "" {
			t.Fatalf("item key is empty: %+v", items[0])
		}
		// The message has two products in ext, so the fast path is skipped and
		// the price is re-associated by re-extracting the segment.
		if items[0].Price == nil || items[0].Price.Amount != 98_000_000 {
			t.Fatalf("price = %+v, want the segment price 98M", items[0].Price)
		}
	})
}
