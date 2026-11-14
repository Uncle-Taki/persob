package nlp

import (
	"regexp"
	"strings"

	"github.com/perso/perso-mind/internal/domain"
)

var (
	// Negotiable indicators
	negotiableRegex = regexp.MustCompile(`(?i)(توافقی|تماس|دایرکت|توافق|پیوی|pv|call)`)

	// Explicit currency markers
	rialMarkerRegex  = regexp.MustCompile(`(?i)(ریال|rial)`)
	tomanMarkerRegex = regexp.MustCompile(`(?i)(تومان|تومن|ت|toman)`)

	// Pattern 1: Compound million + thousand: "۹۸ میلیون و ۵۰۰ هزار تومان"
	compoundPatternRegex = regexp.MustCompile(`(?i)([0-9۰-۹]+)\s*میلیون\s*(?:و\s*)?([0-9۰-۹]+)\s*هزار\s*(تومان|تومن|ت|ریال|toman|rial)?`)

	// Pattern 2: Multiplier with scale word: "98.5 میلیون تومان", "98 میلیون", "98 م ت", "98 م"
	scalePatternRegex = regexp.MustCompile(`(?i)(?:قیمت\s*[:=-]?\s*)?([0-9۰-۹]+(?:[.,/٫][0-9۰-۹]+)?)\s*(میلیون|میلیارد|هزار|همت|م|k)\s*(تومان|تومن|ت|ریال|toman|rial)?`)

	// Pattern 3: Full formatted digit with thousand separators: "98,000,000 تومان", "98.000.000 ت"
	fullNumberRegex = regexp.MustCompile(`(?i)(?:قیمت\s*[:=-]?\s*)?([0-9۰-۹]{1,3}(?:[,\.،][0-9۰-۹]{3}){1,4})\s*(تومان|تومن|ت|ریال|toman|rial)?`)

	// Exclude indicators for false positives (avoid matching "اولترا")
	storageRegex = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:[0-9۰-۹]+\s*(?:گیگ|gig|gb|ترابایت|tb)|ram|رم)(?:$|[^\p{L}\p{N}])`)
	batteryRegex = regexp.MustCompile(`(?i)(?:سلامت|هلث|health|باتری|باطری)\s*[:=-]?\s*[0-9۰-۹]+%?`)
	phoneRegex   = regexp.MustCompile(`(?i)(?:09[0-9]{9}|۰۹[۰-۹]{9})`)
)

type ParsedPrice struct {
	Amount       int64
	Currency     domain.Currency
	Raw          string
	Negotiable   bool
	Confidence   float64
	EvidenceSpan string
}

// ParsePrices finds and extracts all prices mentioned in the text
func ParsePrices(text string) []ParsedPrice {
	if strings.TrimSpace(text) == "" {
		return nil
	}

	normText := Normalize(text)
	var results []ParsedPrice
	var matchedSpans [][2]int

	isOverlapping := func(start, end int) bool {
		for _, span := range matchedSpans {
			if !(end <= span[0] || start >= span[1]) {
				return true
			}
		}
		return false
	}

	// Check negotiable first
	if negotiableRegex.MatchString(normText) {
		loc := negotiableRegex.FindStringIndex(normText)
		matchStr := normText[loc[0]:loc[1]]
		results = append(results, ParsedPrice{
			Amount:       0,
			Currency:     domain.CurrencyToman,
			Raw:          matchStr,
			Negotiable:   true,
			Confidence:   0.90,
			EvidenceSpan: matchStr,
		})
	}

	// 1. Compound pattern: "۹۸ میلیون و ۵۰۰ هزار تومان"
	compoundMatches := compoundPatternRegex.FindAllStringSubmatchIndex(normText, -1)
	for _, m := range compoundMatches {
		span := normText[m[0]:m[1]]
		milStr := PersianAndArabicDigitsToEnglish(normText[m[2]:m[3]])
		hezStr := PersianAndArabicDigitsToEnglish(normText[m[4]:m[5]])
		currStr := ""
		if m[6] != -1 && m[7] != -1 {
			currStr = normText[m[6]:m[7]]
		}

		mil, _ := ParsePersianNumber(milStr)
		hez, _ := ParsePersianNumber(hezStr)
		totalAmount := (mil * 1_000_000) + (hez * 1_000)

		curr := domain.CurrencyToman
		if rialMarkerRegex.MatchString(currStr) {
			curr = domain.CurrencyRial
		}

		matchedSpans = append(matchedSpans, [2]int{m[0], m[1]})
		results = append(results, ParsedPrice{
			Amount:       totalAmount,
			Currency:     curr,
			Raw:          span,
			Negotiable:   false,
			Confidence:   0.95,
			EvidenceSpan: span,
		})
	}

	// 2. Full digit with commas: "98,500,000 تومان"
	fullMatches := fullNumberRegex.FindAllStringSubmatchIndex(normText, -1)
	for _, m := range fullMatches {
		if isOverlapping(m[0], m[1]) {
			continue
		}

		span := normText[m[0]:m[1]]
		numRaw := normText[m[2]:m[3]]
		currStr := ""
		if m[4] != -1 && m[5] != -1 {
			currStr = normText[m[4]:m[5]]
		}

		amount, err := ParsePersianNumber(numRaw)
		if err != nil || amount <= 0 {
			continue
		}

		// Reject numbers that look like phone numbers or dates
		if amount > 1900 && amount < 2100 && currStr == "" {
			continue // Year
		}

		curr := domain.CurrencyToman
		if rialMarkerRegex.MatchString(currStr) {
			curr = domain.CurrencyRial
		}

		// Confidence is higher if explicit currency is given
		conf := 0.90
		if currStr == "" {
			if amount < 10000 {
				continue // Likely not a price
			}
			conf = 0.75
		}

		matchedSpans = append(matchedSpans, [2]int{m[0], m[1]})
		results = append(results, ParsedPrice{
			Amount:       amount,
			Currency:     curr,
			Raw:          span,
			Negotiable:   false,
			Confidence:   conf,
			EvidenceSpan: span,
		})
	}

	// 3. Scale pattern: "۹۸ میلیون تومان", "۹۸ م ت", "۹۸ م"
	scaleMatches := scalePatternRegex.FindAllStringSubmatchIndex(normText, -1)
	for _, m := range scaleMatches {
		if isOverlapping(m[0], m[1]) {
			continue
		}

		span := normText[m[0]:m[1]]
		numStr := normText[m[2]:m[3]]
		unitStr := normText[m[4]:m[5]]
		currStr := ""
		if m[6] != -1 && m[7] != -1 {
			currStr = normText[m[6]:m[7]]
		}

		amount, ok := ExtractMultipliedValue(numStr, unitStr)
		if !ok || amount <= 0 {
			continue
		}

		curr := domain.CurrencyToman
		if rialMarkerRegex.MatchString(currStr) {
			curr = domain.CurrencyRial
		}

		conf := 0.92
		if unitStr == "م" && currStr == "" {
			conf = 0.85
		}

		matchedSpans = append(matchedSpans, [2]int{m[0], m[1]})
		results = append(results, ParsedPrice{
			Amount:       amount,
			Currency:     curr,
			Raw:          span,
			Negotiable:   false,
			Confidence:   conf,
			EvidenceSpan: span,
		})
	}

	return deduplicatePrices(results)
}

func getSurroundingContext(text string, start, end, window int) string {
	s := start - window
	if s < 0 {
		s = 0
	}
	e := end + window
	if e > len(text) {
		e = len(text)
	}
	return text[s:e]
}

func deduplicatePrices(prices []ParsedPrice) []ParsedPrice {
	if len(prices) <= 1 {
		return prices
	}

	seenAmounts := make(map[int64]bool)
	var filtered []ParsedPrice

	for _, p := range prices {
		if !seenAmounts[p.Amount] {
			seenAmounts[p.Amount] = true
			filtered = append(filtered, p)
		}
	}
	return filtered
}
