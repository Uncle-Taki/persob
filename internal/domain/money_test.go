package domain

import "testing"

func TestTomanAmount(t *testing.T) {
	tests := []struct {
		name     string
		amount   int64
		currency Currency
		want     int64
	}{
		{"rial positive multiple", 98_000_000, CurrencyRial, 9_800_000},
		{"rial truncates remainder toward zero", 99, CurrencyRial, 9},
		{"rial truncates negative remainder toward zero", -99, CurrencyRial, -9},
		{"rial zero", 0, CurrencyRial, 0},
		{"rial single digit stays zero", 9, CurrencyRial, 0},
		{"toman identity", 98_000_000, CurrencyToman, 98_000_000},
		{"toman zero", 0, CurrencyToman, 0},
		{"toman negative", -123, CurrencyToman, -123},
		{"unknown currency treated as identity", -42, Currency("USD"), -42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TomanAmount(tt.amount, tt.currency)
			if got != tt.want {
				t.Errorf("TomanAmount(%d, %s) = %d, want %d", tt.amount, tt.currency, got, tt.want)
			}
		})
	}
}
