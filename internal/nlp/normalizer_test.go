package nlp_test

import (
	"testing"

	"github.com/perso/perso-mind/internal/nlp"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Arabic Yeh and Kaf conversion",
			input:    "يک عدد گوشي همراه",
			expected: "یک عدد گوشی همراه",
		},
		{
			name:     "Persian and Arabic digits to English",
			input:    "آیفون ۱۵ با قیمت ۹۸۰۰۰۰۰۰ تومان",
			expected: "آیفون 15 با قیمت 98000000 تومان",
		},
		{
			name:     "Punctuation normalization and quotes",
			input:    "«آیفون»، نو؛ قیمت؟",
			expected: "\"آیفون\" ، نو ؛ قیمت ؟",
		},
		{
			name:     "Excessive whitespace and newlines",
			input:    "  فروش   فوری     آیفون\n\n\n\nدر حد نو   ",
			expected: "فروش فوری آیفون\n\nدر حد نو",
		},
		{
			name:     "ZWNJ with surrounding whitespace cleaned",
			input:    "می‌ خواهم",
			expected: "می خواهم",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nlp.Normalize(tt.input)
			if got != tt.expected {
				t.Errorf("Normalize() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestPersianAndArabicDigitsToEnglish(t *testing.T) {
	input := "۰۱۲۳۴۵۶۷۸۹ ٠١٢٣٤٥٦٧٨٩ 0123456789"
	expected := "0123456789 0123456789 0123456789"
	got := nlp.PersianAndArabicDigitsToEnglish(input)
	if got != expected {
		t.Errorf("PersianAndArabicDigitsToEnglish() = %q, want %q", got, expected)
	}
}

func TestCleanSearchKey(t *testing.T) {
	input := "«آیفون ۱۵ پرو» (۲۵۶ گیگ)!"
	expected := "آیفون 15 پرو 256 گیگ"
	got := nlp.CleanSearchKey(input)
	if got != expected {
		t.Errorf("CleanSearchKey() = %q, want %q", got, expected)
	}
}
