package nlp_test

import (
	"testing"

	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/nlp"
)

func TestParsePrices(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedCount int
		expectedPrice int64
		expectedCurr  domain.Currency
		isNegotiable  bool
	}{
		{
			name:          "Million Toman explicit",
			input:         "آیفون ۱۵ پرو ۲۵۶ تمیز، ۹۸ میلیون تومان",
			expectedCount: 1,
			expectedPrice: 98_000_000,
			expectedCurr:  domain.CurrencyToman,
			isNegotiable:  false,
		},
		{
			name:          "Shorthand million (م ت)",
			input:         "فروش اس ۲۴ اولترا قیمت ۶۵ م ت مقطوع",
			expectedCount: 1,
			expectedPrice: 65_000_000,
			expectedCurr:  domain.CurrencyToman,
			isNegotiable:  false,
		},
		{
			name:          "Full number with separators",
			input:         "دستگاه در حد نو قیمت: ۹۸,۵۰۰,۰۰۰ تومان",
			expectedCount: 1,
			expectedPrice: 98_500_000,
			expectedCurr:  domain.CurrencyToman,
			isNegotiable:  false,
		},
		{
			name:          "Compound million and thousand",
			input:         "قیمت ۹۸ میلیون و ۵۰۰ هزار تومان",
			expectedCount: 1,
			expectedPrice: 98_500_000,
			expectedCurr:  domain.CurrencyToman,
			isNegotiable:  false,
		},
		{
			name:          "Negotiable price",
			input:         "آیفون ۱۳ پرومکس رنگ آبی قیمت توافقی",
			expectedCount: 1,
			expectedPrice: 0,
			expectedCurr:  domain.CurrencyToman,
			isNegotiable:  true,
		},
		{
			name:          "Explicit Rial currency",
			input:         "فروش لپ تاپ مک بوک ۹۸۰,۰۰۰,۰۰۰ ریال",
			expectedCount: 1,
			expectedPrice: 980_000_000,
			expectedCurr:  domain.CurrencyRial,
			isNegotiable:  false,
		},
		{
			name:          "Ignore storage number (not a price)",
			input:         "آیفون 15 با ظرفیت 256 گیگابایت موجود است",
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prices := nlp.ParsePrices(tt.input)
			if len(prices) != tt.expectedCount {
				t.Fatalf("ParsePrices(%q) returned %d prices, want %d", tt.input, len(prices), tt.expectedCount)
			}
			if tt.expectedCount > 0 {
				p := prices[0]
				if p.Amount != tt.expectedPrice {
					t.Errorf("Amount = %d, want %d", p.Amount, tt.expectedPrice)
				}
				if p.Currency != tt.expectedCurr {
					t.Errorf("Currency = %s, want %s", p.Currency, tt.expectedCurr)
				}
				if p.Negotiable != tt.isNegotiable {
					t.Errorf("Negotiable = %v, want %v", p.Negotiable, tt.isNegotiable)
				}
			}
		})
	}
}
