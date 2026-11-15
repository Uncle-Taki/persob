package nlp

import "testing"

func TestParsePersianFloat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    float64
		wantErr bool
	}{
		{"ascii", "3.14", 3.14, false},
		{"persian digits", "۳.۱۴", 3.14, false},
		{"persian decimal separator errors", "۳٫۱۴", 0, true},
		{"arabic digits", "٣.١٤", 3.14, false},
		{"persian comma removed", "۱۲,۳۴", 1234, false},
		{"persian comma separator removed", "۱۲،۳۴", 1234, false},
		{"thousand separators removed", "98,500,000", 98500000, false},
		{"zero", "۰", 0, false},
		{"negative", "-۷.۵", -7.5, false},
		{"surrounding spaces", " 1.5 ", 1.5, false},
		{"empty", "", 0, true},
		{"garbage", "abc", 0, true},
		{"two dots", "1.2.3", 0, true},
		{"huge value (persian 1 with 24 zeros)", "۱۰۰۰۰۰۰۰۰۰۰۰۰۰۰۰۰۰۰۰۰۰۰۰۰", 1e24, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePersianFloat(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParsePersianFloat(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParsePersianFloat(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractMultipliedValue(t *testing.T) {
	tests := []struct {
		name   string
		numStr string
		unit   string
		want   int64
		wantOk bool
	}{
		{"billion persian", "۹۸", "میلیارد", 98_000_000_000, true},
		{"billion short b", "98", "b", 98_000_000_000, true},
		{"billion trailing space unit", "98", "  میلیارد  ", 98_000_000_000, true},
		{"million persian", "۹۸.۵", "میلیون", 98_500_000, true},
		{"million short m", "98", "m", 98_000_000, true},
		{"million short persian m", "۹۸", "م", 98_000_000, true},
		{"thousand persian", "۵۰۰", "هزار", 500_000, true},
		{"thousand short k", "500", "k", 500_000, true},
		{"thousand short persian k", "500", "ک", 500_000, true},
		{"hemmat", "۲", "همت", 2_000_000_000_000, true},
		{"fractional million", "1.5", "میلیون", 1_500_000, true},
		{"fractional hemmat", "۰٫۵", "همت", 500_000_000_000, true},
		{"slash as decimal", "1/5", "هزار", 1_500, true},
		{"persian decimal separator", "۱٫۵", "م", 1_500_000, true},
		{"unknown unit is identity", "۱۲۳", "تومان", 123, true},
		{"empty unit is identity", "۱۲۳", "", 123, true},
		{"invalid number", "abc", "میلیون", 0, false},
		{"empty number", "", "میلیون", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ExtractMultipliedValue(tt.numStr, tt.unit)
			if ok != tt.wantOk {
				t.Fatalf("ExtractMultipliedValue(%q, %q) ok = %v, want %v", tt.numStr, tt.unit, ok, tt.wantOk)
			}
			if ok && got != tt.want {
				t.Errorf("ExtractMultipliedValue(%q, %q) = %d, want %d", tt.numStr, tt.unit, got, tt.want)
			}
		})
	}
}
