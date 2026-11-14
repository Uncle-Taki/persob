package extraction_test

import (
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/extraction"
	"testing"
)

func TestEnglishAndMixedBuyerRequestsAreNotSellerAds(t *testing.T) {
	for _, text := range []string{"WTB iPhone 15 Pro budget 98,000,000 Toman", "Wanted آیفون ۱۵ پرو قیمت ۹۸ میلیون تومان"} {
		r := extraction.ClassifyMessage(text)
		if r.Classification != domain.ClassificationBuyer {
			t.Errorf("buyer request classified %s: %q", r.Classification, text)
		}
	}
}
