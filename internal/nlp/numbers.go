package nlp

import (
	"strconv"
	"strings"
)

// ParsePersianNumber converts strings with Persian/Arabic/English digits and comma separators to int64
func ParsePersianNumber(s string) (int64, error) {
	// First convert digits to ASCII
	s = PersianAndArabicDigitsToEnglish(s)
	// Remove commas, periods (if thousand separator), spaces
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "،", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.TrimSpace(s)

	return strconv.ParseInt(s, 10, 64)
}

// ParsePersianFloat converts number strings with potential decimal points to float64
func ParsePersianFloat(s string) (float64, error) {
	s = PersianAndArabicDigitsToEnglish(s)
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "،", "")
	s = strings.TrimSpace(s)

	return strconv.ParseFloat(s, 64)
}

// ExtractMultipliedValue parses numbers with units like "میلیون", "میلیارد", "هزار", "م"
func ExtractMultipliedValue(numStr, unitStr string) (int64, bool) {
	numStr = PersianAndArabicDigitsToEnglish(numStr)
	numStr = strings.ReplaceAll(numStr, "/", ".")
	numStr = strings.ReplaceAll(numStr, "٫", ".") // Persian decimal separator
	numStr = strings.TrimSpace(numStr)

	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, false
	}

	unitStr = strings.TrimSpace(unitStr)
	switch {
	case strings.Contains(unitStr, "میلیارد") || unitStr == "b":
		return int64(val * 1_000_000_000), true
	case strings.Contains(unitStr, "میلیون") || unitStr == "م" || unitStr == "m":
		return int64(val * 1_000_000), true
	case strings.Contains(unitStr, "هزار") || unitStr == "k" || unitStr == "ک":
		return int64(val * 1_000), true
	case strings.Contains(unitStr, "همت"):
		// 1 Hemmat = 1,000 Billion Tomans
		return int64(val * 1_000_000_000_000), true
	default:
		return int64(val), true
	}
}
