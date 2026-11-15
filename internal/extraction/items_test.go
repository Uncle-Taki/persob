package extraction_test

import (
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/extraction"
	"testing"
)

func TestItemPricesStayWithTheirOwnLines(t *testing.T) {
	msg := &domain.TelegramMessage{MessageText: "فروش ویژه\nآیفون ۱۵ پرو ۲۵۶ گیگ قیمت ۹۸ میلیون تومان\nسامسونگ اس ۲۴ اولترا ۵۱۲ گیگ بدون قیمت"}
	ext := extraction.NewExtractor().ExtractFromMessage(msg)
	items := extraction.ListingItems(msg, ext)
	if len(items) != 2 {
		t.Fatalf("items=%+v", items)
	}
	if items[0].Price == nil || items[0].Price.Amount != 98000000 {
		t.Fatalf("first item price=%+v", items[0].Price)
	}
	if items[1].Price != nil {
		t.Fatalf("unpriced item inherited another item's price: %+v", items[1])
	}
	if ext.Branded || ext.BrandedKnown || items[0].Product.Manufacturer != "" {
		t.Fatal("unknown seller facts were fabricated")
	}
}
func TestItemIdentitySurvivesLineReordering(t *testing.T) {
	a := "آیفون ۱۵ پرو ۲۵۶ گیگ قیمت ۹۸ میلیون تومان"
	b := "سامسونگ اس ۲۴ اولترا ۵۱۲ گیگ قیمت ۸۵ میلیون تومان"
	msg := &domain.TelegramMessage{MessageText: "فروش\n" + a + "\n" + b}
	first := extraction.ListingItems(msg, extraction.NewExtractor().ExtractFromMessage(msg))
	msg.MessageText = "فروش\n" + b + "\n" + a
	next := extraction.ListingItems(msg, extraction.NewExtractor().ExtractFromMessage(msg))
	if len(first) != 2 || len(next) != 2 || first[0].Key != next[1].Key || first[1].Key != next[0].Key {
		t.Fatalf("unstable item identity: %+v %+v", first, next)
	}
}
