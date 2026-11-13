package extraction_test

import (
	"testing"

	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/extraction"
)

func TestClassifyMessage(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedClass domain.Classification
		minConfidence float64
	}{
		{
			name:          "Seller advertisement",
			input:         "فروش فوری آیفون ۱۵ پرو ۲۵۶ تمیز، قیمت ۹۸ میلیون تومان علاءالدین",
			expectedClass: domain.ClassificationSeller,
			minConfidence: 0.80,
		},
		{
			name:          "Buyer request",
			input:         "خریدار یک عدد آیفون ۱۳ پرو سالم بدون بازشدگی هستم، خریدار فوری",
			expectedClass: domain.ClassificationBuyer,
			minConfidence: 0.80,
		},
		{
			name:          "Spam or group rules (neither)",
			input:         "قوانین گروه: هرگونه تبلیغات بدون هماهنگی با ادمین ممنوع است",
			expectedClass: domain.ClassificationNeither,
			minConfidence: 0.70,
		},
		{
			name:          "Uncertain text",
			input:         "سلام کسی می‌دونه قیمت دلار چنده امروز؟",
			expectedClass: domain.ClassificationUncertain,
			minConfidence: 0.30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := extraction.ClassifyMessage(tt.input)
			if res.Classification != tt.expectedClass {
				t.Errorf("ClassifyMessage() = %v, want %v", res.Classification, tt.expectedClass)
			}
			if res.Confidence < tt.minConfidence {
				t.Errorf("Confidence = %f, want >= %f", res.Confidence, tt.minConfidence)
			}
		})
	}
}
