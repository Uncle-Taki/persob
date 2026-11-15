package nlp

import (
	"testing"

	"github.com/perso/perso-mind/internal/domain"
)

func TestParsePricesEmpty(t *testing.T) {
	if got := ParsePrices(""); got != nil {
		t.Errorf("ParsePrices(\"\") = %v, want nil", got)
	}
	if got := ParsePrices("   \t\n  "); got != nil {
		t.Errorf("ParsePrices(whitespace) = %v, want nil", got)
	}
}

// Compound patterns.
func TestParsePricesCompound(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantAmt  int64
		wantCurr domain.Currency
		wantConf float64
	}{
		{
			name:     "million and thousand with currency",
			input:    "قیمت ۹۸ میلیون و ۵۰۰ هزار تومان",
			wantAmt:  98_500_000,
			wantCurr: domain.CurrencyToman,
			wantConf: 0.95,
		},
		{
			name:     "rial currency",
			input:    "۹۸ میلیون و ۵۰۰ هزار ریال",
			wantAmt:  98_500_000,
			wantCurr: domain.CurrencyRial,
			wantConf: 0.95,
		},
		{
			name:     "no currency word (toman default)",
			input:    "۹۸ میلیون و ۵۰۰ هزار",
			wantAmt:  98_500_000,
			wantCurr: domain.CurrencyToman,
			wantConf: 0.95,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prices := ParsePrices(tt.input)
			if len(prices) != 1 {
				t.Fatalf("ParsePrices(%q) returned %d prices, want 1: %+v", tt.input, len(prices), prices)
			}
			p := prices[0]
			if p.Amount != tt.wantAmt {
				t.Errorf("Amount = %d, want %d", p.Amount, tt.wantAmt)
			}
			if p.Currency != tt.wantCurr {
				t.Errorf("Currency = %s, want %s", p.Currency, tt.wantCurr)
			}
			if p.Confidence != tt.wantConf {
				t.Errorf("Confidence = %v, want %v", p.Confidence, tt.wantConf)
			}
			if p.Raw == "" {
				t.Error("Raw is empty")
			}
			if p.EvidenceSpan == "" {
				t.Error("EvidenceSpan is empty")
			}
			if p.Negotiable {
				t.Error("Negotiable should be false")
			}
		})
	}
}

// Full-number (comma-separated) patterns, including every guard branch.
func TestParsePricesFullNumber(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCount int
		wantAmt   int64
		wantCurr  domain.Currency
		wantConf  float64
	}{
		{
			name:      "explicit currency high confidence",
			input:     "۹۸,۵۰۰,۰۰۰ تومان",
			wantCount: 1,
			wantAmt:   98_500_000,
			wantCurr:  domain.CurrencyToman,
			wantConf:  0.90,
		},
		{
			name:      "explicit rial",
			input:     "۹۸۰,۰۰۰,۰۰۰ ریال",
			wantCount: 1,
			wantAmt:   980_000_000,
			wantCurr:  domain.CurrencyRial,
			wantConf:  0.90,
		},
		{
			name:      "no currency and large amount low confidence",
			input:     "۹۸,۵۰۰,۰۰۰",
			wantCount: 1,
			wantAmt:   98_500_000,
			wantCurr:  domain.CurrencyToman,
			wantConf:  0.75,
		},
		{
			name:      "no currency but small amount rejected",
			input:     "۱,۰۰۰",
			wantCount: 0,
		},
		{
			name:      "in year range without currency rejected",
			input:     "۱,۹۹۹",
			wantCount: 0,
		},
		{
			name:      "in year range but explicit currency accepted",
			input:     "۱,۹۹۹ تومان",
			wantCount: 1,
			wantAmt:   1_999,
			wantCurr:  domain.CurrencyToman,
			wantConf:  0.90,
		},
		{
			name:      "zero amount (0,000) rejected",
			input:     "۰,۰۰۰",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prices := ParsePrices(tt.input)
			if len(prices) != tt.wantCount {
				t.Fatalf("ParsePrices(%q) returned %d prices, want %d: %+v", tt.input, len(prices), tt.wantCount, prices)
			}
			if tt.wantCount == 1 {
				p := prices[0]
				if p.Amount != tt.wantAmt {
					t.Errorf("Amount = %d, want %d", p.Amount, tt.wantAmt)
				}
				if p.Currency != tt.wantCurr {
					t.Errorf("Currency = %s, want %s", p.Currency, tt.wantCurr)
				}
				if p.Confidence != tt.wantConf {
					t.Errorf("Confidence = %v, want %v", p.Confidence, tt.wantConf)
				}
			}
		})
	}
}

// A full-number match overlapping an already recorded compound span is
// skipped by the isOverlapping guard.
// In "۹۸,۵۰۰ میلیون و ۵۰۰ هزار تومان" the compound regex matches starting
// at the second digit group ("۵۰۰ میلیون ..."), while the full-number regex
// matches "۹۸,۵۰۰" which overlaps that span and must be dropped.
func TestParsePricesFullNumberOverlapsCompound(t *testing.T) {
	prices := ParsePrices("۹۸,۵۰۰ میلیون و ۵۰۰ هزار تومان")
	if len(prices) != 1 {
		t.Fatalf("ParsePrices returned %d prices, want 1: %+v", len(prices), prices)
	}
	// Compound: 500,000,000 + 500,000.
	if prices[0].Amount != 500_500_000 {
		t.Errorf("Amount = %d, want 500500000", prices[0].Amount)
	}
	if prices[0].Currency != domain.CurrencyToman {
		t.Errorf("Currency = %s, want toman", prices[0].Currency)
	}
	if prices[0].Confidence != 0.95 {
		t.Errorf("Confidence = %v, want 0.95", prices[0].Confidence)
	}
}

// Scale patterns.
func TestParsePricesScale(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantAmt  int64
		wantCurr domain.Currency
		wantConf float64
	}{
		{
			name:     "million with currency",
			input:    "۹۸ میلیون تومان",
			wantAmt:  98_000_000,
			wantCurr: domain.CurrencyToman,
			wantConf: 0.92,
		},
		{
			name:     "billion with currency",
			input:    "۲ میلیارد تومان",
			wantAmt:  2_000_000_000,
			wantCurr: domain.CurrencyToman,
			wantConf: 0.92,
		},
		{
			name:     "thousand k with currency",
			input:    "۵۰۰ k تومان",
			wantAmt:  500_000,
			wantCurr: domain.CurrencyToman,
			wantConf: 0.92,
		},
		{
			name:     "hemmat",
			input:    "۲ همت تومان",
			wantAmt:  2_000_000_000_000,
			wantCurr: domain.CurrencyToman,
			wantConf: 0.92,
		},
		{
			name:     "rial scale",
			input:    "۹۸ میلیون ریال",
			wantAmt:  98_000_000,
			wantCurr: domain.CurrencyRial,
			wantConf: 0.92,
		},
		{
			name:     "shorthand m without currency lower confidence",
			input:    "۶۵ م",
			wantAmt:  65_000_000,
			wantCurr: domain.CurrencyToman,
			wantConf: 0.85,
		},
		{
			name:     "decimal million",
			input:    "98.5 میلیون تومان",
			wantAmt:  98_500_000,
			wantCurr: domain.CurrencyToman,
			wantConf: 0.92,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prices := ParsePrices(tt.input)
			if len(prices) != 1 {
				t.Fatalf("ParsePrices(%q) returned %d prices, want 1: %+v", tt.input, len(prices), prices)
			}
			p := prices[0]
			if p.Amount != tt.wantAmt {
				t.Errorf("Amount = %d, want %d", p.Amount, tt.wantAmt)
			}
			if p.Currency != tt.wantCurr {
				t.Errorf("Currency = %s, want %s", p.Currency, tt.wantCurr)
			}
			if p.Confidence != tt.wantConf {
				t.Errorf("Confidence = %v, want %v", p.Confidence, tt.wantConf)
			}
		})
	}
}

// Zero amounts are rejected by the scale pattern.
func TestParsePricesScaleZeroAmount(t *testing.T) {
	if prices := ParsePrices("۰ میلیون تومان"); len(prices) != 0 {
		t.Errorf("ParsePrices(zero million) returned %d prices, want 0: %+v", len(prices), prices)
	}
}

// Overlap guards: compound matches must suppress later full/scale matches
// that occupy the same span, and multiple distinct prices coexist.
func TestParsePricesOverlappingAndMultiple(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantCounts  int
		wantAmounts []int64
	}{
		{
			name:        "compound suppresses overlapping scale",
			input:       "۹۸ میلیون و ۵۰۰ هزار تومان",
			wantCounts:  1,
			wantAmounts: []int64{98_500_000},
		},
		{
			name:        "two distinct scale prices",
			input:       "اولی ۹۸ میلیون تومان، دومی ۶۵ میلیون تومان",
			wantCounts:  2,
			wantAmounts: []int64{98_000_000, 65_000_000},
		},
		{
			name:        "identical amounts deduplicated",
			input:       "۹۸ میلیون تومان و دوباره ۹۸ میلیون تومان",
			wantCounts:  1,
			wantAmounts: []int64{98_000_000},
		},
		{
			name:        "negotiable plus price",
			input:       "قیمت توافقی یا ۹۸ میلیون تومان",
			wantCounts:  2,
			wantAmounts: []int64{0, 98_000_000},
		},
		{
			name:        "negotiable only",
			input:       "قیمت توافقی",
			wantCounts:  1,
			wantAmounts: []int64{0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prices := ParsePrices(tt.input)
			if len(prices) != tt.wantCounts {
				t.Fatalf("ParsePrices(%q) returned %d prices, want %d: %+v", tt.input, len(prices), tt.wantCounts, prices)
			}
			for i, want := range tt.wantAmounts {
				if prices[i].Amount != want {
					t.Errorf("prices[%d].Amount = %d, want %d", i, prices[i].Amount, want)
				}
			}
		})
	}
}

// deduplicatePrices: empty, single and multi-element paths.
func TestDeduplicatePrices(t *testing.T) {
	if got := deduplicatePrices(nil); got != nil {
		t.Errorf("deduplicatePrices(nil) = %v, want nil", got)
	}

	single := []ParsedPrice{{Amount: 1}}
	got := deduplicatePrices(single)
	if len(got) != 1 || got[0].Amount != 1 {
		t.Errorf("deduplicatePrices(single) = %+v, want unchanged", got)
	}

	multi := []ParsedPrice{
		{Amount: 1},
		{Amount: 1},
		{Amount: 2},
		{Amount: 3},
		{Amount: 2},
	}
	got = deduplicatePrices(multi)
	if len(got) != 3 {
		t.Fatalf("deduplicatePrices(multi) returned %d, want 3: %+v", len(got), got)
	}
	if got[0].Amount != 1 || got[1].Amount != 2 || got[2].Amount != 3 {
		t.Errorf("deduplicatePrices(multi) = %v, want [1 2 3]", []int64{got[0].Amount, got[1].Amount, got[2].Amount})
	}
}

// getSurroundingContext: window clamping at both ends and no clamping.
func TestGetSurroundingContext(t *testing.T) {
	text := "0123456789" // indices 0..9

	if got := getSurroundingContext(text, 2, 5, 100); got != text {
		t.Errorf("both ends clamped: got %q, want %q", got, text)
	}
	// end is an exclusive index: window expands to [start-w, end+w).
	if got := getSurroundingContext(text, 2, 5, 1); got != "12345" {
		t.Errorf("no clamp: got %q, want %q", got, "12345")
	}
	if got := getSurroundingContext(text, 0, 2, 5); got != "0123456" {
		t.Errorf("end clamped: got %q, want %q", got, "0123456")
	}
	if got := getSurroundingContext(text, 8, 10, 5); got != "3456789" {
		t.Errorf("start clamped: got %q, want %q", got, "3456789")
	}
	if got := getSurroundingContext(text, 4, 4, 2); got != "2345" {
		t.Errorf("zero width: got %q, want %q", got, "2345")
	}
}
