package nlp_test

import (
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/nlp"
	"testing"
)

func TestEnglishRialUnitIsPreserved(t *testing.T) {
	p := nlp.ParsePrices("For sale iPhone 15 Pro price 980,000,000 Rial")
	if len(p) != 1 || p[0].Amount != 980_000_000 || p[0].Currency != domain.CurrencyRial {
		t.Fatalf("English Rial price mislabeled: %+v", p)
	}
}
