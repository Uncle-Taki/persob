package extraction_test

import (
	"testing"
	"time"

	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/extraction"
)

func TestExtractFromMessage(t *testing.T) {
	extractor := extraction.NewExtractor()

	msg := &domain.TelegramMessage{
		TelegramChatID:    -1001234567890,
		TelegramMessageID: 555,
		SenderID:          12345678,
		SenderUsername:    "apple_seller_tehran",
		SenderDisplayName: "تهران اپل",
		MessageText:       "فروش یک دستگاه آیفون ۱۵ پرو ۲۵۶ گیگ در حد نو رنگ تیتانیوم، قیمت ۹۸ میلیون تومان. تحویل پاساژ علاءالدین",
		MessageDate:       time.Now(),
		ReceivedAt:        time.Now(),
	}

	result := extractor.ExtractFromMessage(msg)

	if result.Classification != domain.ClassificationSeller {
		t.Fatalf("Classification = %v, want %v", result.Classification, domain.ClassificationSeller)
	}

	if len(result.ExtractedProducts) == 0 {
		t.Fatalf("Expected at least 1 extracted product")
	}

	prod := result.ExtractedProducts[0]
	if prod.Brand != "Apple" {
		t.Errorf("Product Brand = %s, want Apple", prod.Brand)
	}
	if prod.Model != "15 Pro" {
		t.Errorf("Product Model = %s, want 15 Pro", prod.Model)
	}
	if prod.Variant != "256GB" {
		t.Errorf("Product Variant = %s, want 256GB", prod.Variant)
	}
	if prod.Condition != domain.ConditionLikeNew {
		t.Errorf("Product Condition = %s, want like_new", prod.Condition)
	}

	if len(result.ExtractedPrices) == 0 {
		t.Fatalf("Expected at least 1 extracted price")
	}

	price := result.ExtractedPrices[0]
	if price.Amount != 98_000_000 {
		t.Errorf("Price Amount = %d, want 98000000", price.Amount)
	}
	if price.Currency != domain.CurrencyToman {
		t.Errorf("Price Currency = %s, want IRR_TOMAN", price.Currency)
	}

	if result.Location == nil {
		t.Fatalf("Expected extracted location")
	}
	if result.Location.City != "تهران" {
		t.Errorf("Location City = %s, want تهران", result.Location.City)
	}
	if result.Location.Area != "علاءالدین" {
		t.Errorf("Location Area = %s, want علاءالدین", result.Location.Area)
	}
}
