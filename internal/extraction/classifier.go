package extraction

import (
	"regexp"
	"strings"

	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/nlp"
)

var (
	// Seller keywords
	sellerPattern = regexp.MustCompile(`(?i)(\b(?:selling|for sale|wts)\b|فروش|فروش فوری|فروشی|موجود داریم|موجود است|موجود میباشد|آکبند|پلمپ|پلمب|کارکرده|دست دوم|برای فروش|stock|فروشگاه|استوک|ارسال به سراسر|قیمت مقطوع|فروشنده)`)

	// Buyer keywords
	buyerPattern = regexp.MustCompile(`(?i)(\b(?:wtb|buying|wanted|looking for)\b|خریدار|خریدارم|خریدار فوری|نیازمند|خریداری|کسی داره|خرید|میخوام|خریداریم|طالب|خریدار نقدی|نیاز دارم|خریدار دستگاه)`)

	// Spam / Neither keywords (announcements, rules, links, admin notes)
	neitherPattern = regexp.MustCompile(`(?i)(قوانین گروه|قوانین|تبلیغات|ادمین|لینک کانال|عضو شوید|فیلترشکن|vpn|کانفیگ|ارز دیجیتال|سیگنال|صرافی)`)
)

type ClassificationResult struct {
	Classification domain.Classification
	Confidence     float64
	MatchedRule    string
}

// ClassifyMessage determines whether a message is Seller, Buyer, Neither, or Uncertain
func ClassifyMessage(text string) ClassificationResult {
	if strings.TrimSpace(text) == "" {
		return ClassificationResult{
			Classification: domain.ClassificationNeither,
			Confidence:     1.0,
			MatchedRule:    "empty_text",
		}
	}

	normText := nlp.CleanSearchKey(text)

	// Check if clearly non-marketplace / group rules / admin spam (neither)
	if neitherPattern.MatchString(normText) {
		return ClassificationResult{
			Classification: domain.ClassificationNeither,
			Confidence:     0.90,
			MatchedRule:    "neither_keywords",
		}
	}

	isBuyer := buyerPattern.MatchString(normText)
	isSeller := sellerPattern.MatchString(normText)

	// Conflict: both buyer and seller patterns matched
	if isBuyer && isSeller {
		// Differentiate based on dominant opening keywords
		firstWords := strings.Fields(normText)
		if len(firstWords) > 0 {
			first := firstWords[0]
			if strings.HasPrefix(first, "خریدار") {
				return ClassificationResult{
					Classification: domain.ClassificationBuyer,
					Confidence:     0.80,
					MatchedRule:    "buyer_prefix_priority",
				}
			}
			if strings.HasPrefix(first, "فروش") || strings.HasPrefix(first, "فروشی") {
				return ClassificationResult{
					Classification: domain.ClassificationSeller,
					Confidence:     0.80,
					MatchedRule:    "seller_prefix_priority",
				}
			}
		}
		// Inconclusive conflict: do not force
		return ClassificationResult{
			Classification: domain.ClassificationUncertain,
			Confidence:     0.50,
			MatchedRule:    "buyer_seller_conflict",
		}
	}

	if isBuyer {
		return ClassificationResult{
			Classification: domain.ClassificationBuyer,
			Confidence:     0.92,
			MatchedRule:    "buyer_keywords",
		}
	}

	if isSeller {
		return ClassificationResult{
			Classification: domain.ClassificationSeller,
			Confidence:     0.92,
			MatchedRule:    "seller_keywords",
		}
	}

	// Check if an explicit price is present accompanied by commercial terms
	prices := nlp.ParsePrices(text)
	if len(prices) > 0 && prices[0].Amount > 100000 && !strings.Contains(normText, "چنده") && !strings.Contains(normText, "چند است") && !strings.Contains(normText, "می دونه") {
		return ClassificationResult{
			Classification: domain.ClassificationSeller,
			Confidence:     0.75,
			MatchedRule:    "price_present_implied_seller",
		}
	}

	// Cannot determine with certainty
	return ClassificationResult{
		Classification: domain.ClassificationUncertain,
		Confidence:     0.40,
		MatchedRule:    "insufficient_signals",
	}
}
