package extraction_test

import (
	"testing"

	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/extraction"
)

// TestClassifyMessage_MoreCases covers the branches not exercised by the
// original classifier tests: empty text, buyer/seller prefix priority on
// conflicts, inconclusive conflicts, and the price-implied-seller heuristic
// (including its "چنده/چند است" guard).
func TestClassifyMessage_MoreCases(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantClass domain.Classification
		wantConf  float64
		wantRule  string
	}{
		{
			name:      "empty text is neither with full confidence",
			input:     "   ",
			wantClass: domain.ClassificationNeither,
			wantConf:  1.0,
			wantRule:  "empty_text",
		},
		{
			name:      "buyer prefix wins an inconclusive conflict",
			input:     "خریدار آیفون ۱۵ پرو موجود است",
			wantClass: domain.ClassificationBuyer,
			wantConf:  0.80,
			wantRule:  "buyer_prefix_priority",
		},
		{
			name:      "seller prefix wins an inconclusive conflict",
			input:     "فروش آیفون ۱۵ پرو، خریدار فوری",
			wantClass: domain.ClassificationSeller,
			wantConf:  0.80,
			wantRule:  "seller_prefix_priority",
		},
		{
			name:      "conflict without a priority prefix is uncertain",
			input:     "آیفون ۱۵ پرو موجود است، خریدار فوری",
			wantClass: domain.ClassificationUncertain,
			wantConf:  0.50,
			wantRule:  "buyer_seller_conflict",
		},
		{
			name:      "explicit price without keywords implies seller",
			input:     "آیفون ۱۵ پرو ۲۵۶ گیگ، قیمت ۹۸ میلیون تومان",
			wantClass: domain.ClassificationSeller,
			wantConf:  0.75,
			wantRule:  "price_present_implied_seller",
		},
		{
			name:      "asking the price with چنده stays uncertain",
			input:     "قیمت آیفون چنده؟ ۹۸ میلیون تومان",
			wantClass: domain.ClassificationUncertain,
			wantConf:  0.40,
			wantRule:  "insufficient_signals",
		},
		{
			name:      "asking the price with چند است stays uncertain",
			input:     "آیفون ۱۴ چند است؟ ۹۸ میلیون تومان",
			wantClass: domain.ClassificationUncertain,
			wantConf:  0.40,
			wantRule:  "insufficient_signals",
		},
		{
			name:      "asking the price with می دونه stays uncertain",
			input:     "کسی می دونه قیمتش ۹۸ میلیون تومان هست؟",
			wantClass: domain.ClassificationUncertain,
			wantConf:  0.40,
			wantRule:  "insufficient_signals",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := extraction.ClassifyMessage(tt.input)
			if res.Classification != tt.wantClass {
				t.Errorf("ClassifyMessage() = %v, want %v", res.Classification, tt.wantClass)
			}
			if res.Confidence != tt.wantConf {
				t.Errorf("Confidence = %v, want %v", res.Confidence, tt.wantConf)
			}
			if res.MatchedRule != tt.wantRule {
				t.Errorf("MatchedRule = %q, want %q", res.MatchedRule, tt.wantRule)
			}
		})
	}
}
