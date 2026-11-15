package extraction_test

import (
	"testing"
	"time"

	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/extraction"
)

func TestExtractHealthcareProductAndPackMetadata(t *testing.T) {
	msg := &domain.TelegramMessage{
		TelegramChatID: -100123,
		TelegramMessageID: 9,
		SenderID: 7,
		MessageText: "فروش قرص آموکسی سیلین ۵۰۰ میلی گرم بسته ۲۰ عدد، قیمت ۲۵۰ هزار تومان",
		MessageDate: time.Now(),
		ReceivedAt: time.Now(),
	}
	result := extraction.NewExtractor().ExtractFromMessage(msg)
	if len(result.ExtractedProducts) != 1 { t.Fatalf("products=%d, want 1", len(result.ExtractedProducts)) }
	product := result.ExtractedProducts[0]
	if product.Name != "آموکسی سیلین" { t.Fatalf("name=%q", product.Name) }
	if product.DosageForm != "قرص" || product.Strength != "500 میلی گرم" || product.PackSize != 20 {
		t.Fatalf("metadata=%+v", product)
	}
}
