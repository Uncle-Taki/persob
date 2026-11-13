package nlp

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	spaceRegex     = regexp.MustCompile(`[ \t\r\f]+`)
	newlineRegex   = regexp.MustCompile(`\n{3,}`)
	zwnjSpaceRegex = regexp.MustCompile(`\x{200c}[ \t]+|[ \t]+\x{200c}`)
	multiZwnjRegex = regexp.MustCompile(`\x{200c}{2,}`)
)

// NormalizerOptions configures Persian text normalization
type NormalizerOptions struct {
	ConvertDigitsToEnglish  bool
	UnifyArabicPersianChars bool
	NormalizePunctuation    bool
	CleanWhitespace         bool
	CleanZWNJ               bool
}

func DefaultOptions() NormalizerOptions {
	return NormalizerOptions{
		ConvertDigitsToEnglish:  true,
		UnifyArabicPersianChars: true,
		NormalizePunctuation:    true,
		CleanWhitespace:         true,
		CleanZWNJ:               true,
	}
}

// Normalize applies deterministic Persian text normalization rules
func Normalize(text string) string {
	return NormalizeWithOptions(text, DefaultOptions())
}

// NormalizeWithOptions allows custom normalization settings
func NormalizeWithOptions(text string, opts NormalizerOptions) string {
	if text == "" {
		return ""
	}

	result := text

	if opts.UnifyArabicPersianChars {
		result = UnifyChars(result)
	}

	if opts.ConvertDigitsToEnglish {
		result = PersianAndArabicDigitsToEnglish(result)
	}

	if opts.CleanZWNJ {
		// Replace ZWNJ next to spaces with space
		result = zwnjSpaceRegex.ReplaceAllString(result, " ")
		// Collapse multiple ZWNJs into single ZWNJ
		result = multiZwnjRegex.ReplaceAllString(result, "\u200c")
	}

	if opts.NormalizePunctuation {
		result = normalizePunctuation(result)
	}

	if opts.CleanWhitespace {
		// Collapse multi spaces
		result = spaceRegex.ReplaceAllString(result, " ")
		// Collapse more than 2 newlines
		result = newlineRegex.ReplaceAllString(result, "\n\n")
		result = strings.TrimSpace(result)
	}

	return result
}

// UnifyChars replaces Arabic characters with standard Persian equivalents
func UnifyChars(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))

	for _, r := range s {
		switch r {
		// Arabic Yeh to Persian Yeh
		case '\u064A', '\u0649': // ي, ى
			sb.WriteRune('\u06CC') // ی
		// Arabic Kaf to Persian Kaf
		case '\u0643': // ك
			sb.WriteRune('\u06A9') // ک
		// Arabic Teh Marbuta
		case '\u0629': // ة
			sb.WriteRune('\u0647') // ه
		// Arabic Heh with Yeh above
		case '\u06C0': // ۀ
			sb.WriteString("ه‌ی")
		// Tatweel / Kashida (stretching character)
		case '\u0640': // ـ
			// ignore / remove
			continue
		// Arabic vowels/harakat (remove for cleaner search)
		case '\u064B', '\u064C', '\u064D', '\u064E', '\u064F', '\u0650', '\u0651', '\u0652':
			continue
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// PersianAndArabicDigitsToEnglish maps Persian (۰-۹) and Arabic (٠-٩) to ASCII 0-9
func PersianAndArabicDigitsToEnglish(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))

	for _, r := range s {
		switch {
		case r >= '\u06F0' && r <= '\u06F9': // Persian digits ۰-۹
			sb.WriteRune('0' + (r - '\u06F0'))
		case r >= '\u0660' && r <= '\u0669': // Arabic digits ٠-٩
			sb.WriteRune('0' + (r - '\u0660'))
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func normalizePunctuation(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))

	for _, r := range s {
		switch r {
		case '«', '»':
			sb.WriteRune('"')
		case '،':
			sb.WriteString(" ، ")
		case '؟':
			sb.WriteString(" ؟ ")
		case '؛':
			sb.WriteString(" ؛ ")
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// CleanSearchKey returns a lowercase, normalized, punctuation-free string for matching
func CleanSearchKey(s string) string {
	s = Normalize(s)
	s = strings.ToLower(s)
	// Remove punctuation and ZWNJ
	var sb strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			sb.WriteRune(r)
		} else {
			sb.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}
