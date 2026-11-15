package extraction

import (
	"crypto/sha256"
	"fmt"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/nlp"
	"regexp"
	"strings"
)

type ListingItem struct {
	Key     string
	Product domain.ExtractedProduct
	Price   *domain.ExtractedPrice
}

// ListingItems associates money only with the same independently extracted text
// segment. Unassociated prices remain unknown rather than being copied to items.
func ListingItems(msg *domain.TelegramMessage, ext *domain.ExtractedMessage) []ListingItem {
	result := []ListingItem{}
	seen := map[string]bool{}
	segments := regexp.MustCompile(`\n|\s+و\s+|\s*[/|;]\s*`).Split(msg.MessageText, -1)
	for _, product := range ext.ExtractedProducts {
		key := fmt.Sprintf("%x", sha256.Sum256([]byte(nlp.CleanSearchKey(product.Brand+":"+product.Model+":"+product.Variant+":"+product.Name))))
		if seen[key] {
			continue
		}
		seen[key] = true
		item := ListingItem{Key: key, Product: product}
		// A single-item message is unambiguous only if it contains exactly one price.
		if len(ext.ExtractedProducts) == 1 && len(ext.ExtractedPrices) == 1 {
			p := ext.ExtractedPrices[0]
			item.Price = &p
		} else {
			for _, seg := range segments {
				candidates := NewExtractor().ExtractProducts(nlp.Normalize(seg), seg)
				if len(candidates) != 1 {
					continue
				}
				candidate := candidates[0]
				if !strings.EqualFold(candidate.Name, product.Name) || candidate.Variant != product.Variant {
					continue
				}
				prices := nlp.ParsePrices(seg)
				if len(prices) == 1 {
					p := prices[0]
					item.Price = &domain.ExtractedPrice{Amount: p.Amount, Currency: p.Currency, Raw: p.Raw, Confidence: p.Confidence, EvidenceSpan: p.EvidenceSpan, Negotiable: p.Negotiable}
				}
				break
			}
		}
		result = append(result, item)
	}
	return result
}
