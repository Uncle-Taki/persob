package extraction

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/nlp"
)

var (
	// Condition regexes - Note: Check LikeNew before New so "در حد نو" is not captured as New
	condLikeNewRegex  = regexp.MustCompile(`(?i)(در حد نو|در حد آک|مشابه آک|در حد صفر|active|اکتیو در حد)`)
	condNewRegex      = regexp.MustCompile(`(?i)(آکبند|پلمپ|پلمب|صفر|باکس اصلی|not active|نات اکتیو|(?:^|\s)نو(?:\s|$))`)
	condUsedGoodRegex = regexp.MustCompile(`(?i)(تمیز|بسیار تمیز|بدون خط و خش|بدون بازشدگی|battery health|سلامت باتری)`)
	condUsedFairRegex = regexp.MustCompile(`(?i)(کارکرده|دست دوم|سالم|استوک|used)`)
	condUsedPoorRegex = regexp.MustCompile(`(?i)(خط و خش دار|ترک|تعویضی|تعمیر شده|شکسته|مشکل دار)`)

	// Storage variant regex
	storageVariantRegex = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(64|128|256|512|1024)\s*(?:gb|گیگ|gig)?(?:$|[^\p{L}\p{N}])|(?:^|[^\p{L}\p{N}])(1|2)\s*(?:tb|ترا|ترابایت)(?:$|[^\p{L}\p{N}])`)

	// Quantity regex
	quantityRegex = regexp.MustCompile(`(?i)(?:تعداد\s*[:=-]?\s*|\b)([0-9۰-۹]+)\s*(?:عدد|دستگاه|تا|پک)`)

	// Major Iranian cities (avoiding ASCII-only \b)
	citiesRegex = regexp.MustCompile(`(?i)(تهران|مشهد|اصفهان|شیراز|تبریز|کرج|اهواز|قم|رشت|ارومیه|یزد|ساری|کرمان|همدان|کیش)`)

	// Tehran districts / tech markets (avoiding ASCII-only \b)
	tehranAreasRegex    = regexp.MustCompile(`(?i)(علاءالدین|چارسو|پایتخت|بازار موبایل|سعادت آباد|شهرک غرب|تجریش|پونک|صادقیه|ولیعصر|ونک|ستارخان|جنت آباد|تهرانپارس|رسالت|سیدخندان|نارمک)`)
	healthcareFormRegex = regexp.MustCompile(`(?i)(قرص|کپسول|آمپول|امپول|شربت|سرم|پماد|کرم|اسپری|قطره|ماسک|دستکش|گاز|باند|سرنگ|انسولین|لوازم پزشکی)`)
	healthcareNameRegex = regexp.MustCompile(`(?i)(?:قرص|کپسول|آمپول|امپول|شربت|سرم|پماد|کرم|اسپری|قطره|ماسک|دستکش|گاز|باند|سرنگ|انسولین)\s+([\p{L}][\p{L}\x{200c}-]*(?:\s+[\p{L}][\p{L}\x{200c}-]*){0,1})`)
	strengthRegex       = regexp.MustCompile(`(?i)([0-9۰-۹]+\s*(?:mg|میلی\s*گرم|g|گرم|ml|میلی\s*لیتر|٪|%|درصد))`)
	packSizeRegex       = regexp.MustCompile(`(?i)(?:بسته|پک|جعبه)\s*([0-9۰-۹]+)\s*(?:عدد|حلقه|دستگاه)?`)
)

type Extractor struct{}

func NewExtractor() *Extractor {
	return &Extractor{}
}

// ExtractFromMessage performs rule-based deterministic extraction on a Telegram message
func (e *Extractor) ExtractFromMessage(msg *domain.TelegramMessage) *domain.ExtractedMessage {
	text := msg.MessageText
	norm := nlp.Normalize(text)

	clsRes := ClassifyMessage(text)

	extracted := &domain.ExtractedMessage{
		MessageID:         generateMessageID(msg.TelegramChatID, msg.TelegramMessageID),
		Classification:    clsRes.Classification,
		Confidence:        clsRes.Confidence,
		ExtractionVersion: 1,
		CreatedAt:         time.Now(),
		Branded:           false,
		SellerMetadata: map[string]string{
			"sender_id":       strconv.FormatInt(msg.SenderID, 10),
			"sender_username": msg.SenderUsername,
			"display_name":    msg.SenderDisplayName,
			"chat_id":         strconv.FormatInt(msg.TelegramChatID, 10),
		},
	}

	// Extract prices
	parsedPrices := nlp.ParsePrices(text)
	for _, p := range parsedPrices {
		extracted.ExtractedPrices = append(extracted.ExtractedPrices, domain.ExtractedPrice{
			Amount:       p.Amount,
			Currency:     p.Currency,
			Raw:          p.Raw,
			Negotiable:   p.Negotiable,
			Confidence:   p.Confidence,
			EvidenceSpan: p.EvidenceSpan,
		})
	}

	// Extract location
	extracted.Location = e.extractLocation(norm)

	// Extract product info if classified as seller
	if clsRes.Classification == domain.ClassificationSeller || clsRes.Classification == domain.ClassificationUncertain {
		products := e.ExtractProducts(norm, text)
		extracted.ExtractedProducts = products
	}

	return extracted
}

// ExtractProducts extracts all product items found in a message text (multi-product support)
func (e *Extractor) ExtractProducts(normText, originalText string) []domain.ExtractedProduct {
	var products []domain.ExtractedProduct
	seenModels := make(map[string]bool)

	// 1. Try line-by-line / bullet extraction
	lines := strings.Split(originalText, "\n")
	if len(lines) > 1 {
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			normLine := nlp.Normalize(trimmed)
			prod := e.extractProductDetails(normLine, trimmed)
			if prod != nil {
				key := prod.Name + "_" + prod.Variant
				if !seenModels[key] {
					seenModels[key] = true
					products = append(products, *prod)
				}
			}
		}
	}

	// 2. If multi-line found products, return them
	if len(products) > 0 {
		return products
	}

	// 3. Try splitting single line by conjunctions " و " or " / " or " - "
	segments := regexp.MustCompile(`(?:\s+و\s+|\s*[/|;]\s*)`).Split(originalText, -1)
	if len(segments) > 1 {
		for _, seg := range segments {
			trimmed := strings.TrimSpace(seg)
			if len(trimmed) < 4 {
				continue
			}
			normSeg := nlp.Normalize(trimmed)
			prod := e.extractProductDetails(normSeg, trimmed)
			if prod != nil {
				key := prod.Name + "_" + prod.Variant
				if !seenModels[key] {
					seenModels[key] = true
					products = append(products, *prod)
				}
			}
		}
	}

	if len(products) > 0 {
		return products
	}

	// 4. Fallback to full-text extraction
	prod := e.extractProductDetails(normText, originalText)
	if prod != nil {
		products = append(products, *prod)
	}

	return products
}

func (e *Extractor) extractLocation(normText string) *domain.ExtractedLocation {
	var city, area, evidence string

	if loc := tehranAreasRegex.FindStringIndex(normText); loc != nil {
		area = normText[loc[0]:loc[1]]
		city = "تهران"
		evidence = area
	} else if loc := citiesRegex.FindStringIndex(normText); loc != nil {
		city = normText[loc[0]:loc[1]]
		evidence = city
	}

	if city == "" && area == "" {
		return nil
	}

	return &domain.ExtractedLocation{
		City:         city,
		Area:         area,
		LocationText: evidence,
		EvidenceSpan: evidence,
	}
}

func (e *Extractor) extractProductDetails(normText, originalText string) *domain.ExtractedProduct {
	// Detect Condition (Check LikeNew before New)
	condition := domain.ConditionUnknown
	switch {
	case condLikeNewRegex.MatchString(normText):
		condition = domain.ConditionLikeNew
	case condNewRegex.MatchString(normText):
		condition = domain.ConditionNew
	case condUsedGoodRegex.MatchString(normText):
		condition = domain.ConditionUsedGood
	case condUsedFairRegex.MatchString(normText):
		condition = domain.ConditionUsedFair
	case condUsedPoorRegex.MatchString(normText):
		condition = domain.ConditionUsedPoor
	}

	// Detect Variant (e.g. 256GB, 1TB)
	var variant string
	if m := storageVariantRegex.FindStringSubmatch(normText); len(m) > 0 {
		if len(m) > 1 && m[1] != "" {
			variant = m[1] + "GB"
		} else if len(m) > 2 && m[2] != "" {
			variant = m[2] + "TB"
		}
	}

	// Detect Quantity
	quantity := 1
	if m := quantityRegex.FindStringSubmatch(normText); len(m) > 1 {
		if q, err := nlp.ParsePersianNumber(m[1]); err == nil && q > 0 && q < 1000 {
			quantity = int(q)
		}
	}

	// Brand and Model detection heuristics
	brand, model, name := detectBrandAndModel(normText)
	if name == "" {
		return e.extractHealthcareProduct(normText, originalText, condition, quantity)
	}

	return &domain.ExtractedProduct{
		Name:         name,
		Brand:        brand,
		Manufacturer: "",
		Model:        model,
		Variant:      variant,
		Condition:    condition,
		Quantity:     quantity,
		Confidence:   0.85,
		EvidenceSpan: name,
	}
}

func (e *Extractor) extractHealthcareProduct(normText, originalText string, condition domain.ItemCondition, quantity int) *domain.ExtractedProduct {
	form := healthcareFormRegex.FindString(normText)
	if form == "" {
		return nil
	}
	m := healthcareNameRegex.FindStringSubmatch(normText)
	name := ""
	if len(m) > 1 {
		name = strings.TrimSpace(m[1])
	}
	if name == "" {
		name = form
	}
	strength := ""
	if sm := strengthRegex.FindStringSubmatch(normText); len(sm) > 1 {
		strength = strings.TrimSpace(sm[1])
	}
	pack := 0
	if pm := packSizeRegex.FindStringSubmatch(normText); len(pm) > 1 {
		if n, err := nlp.ParsePersianNumber(pm[1]); err == nil && n > 0 && n < 100000 {
			pack = int(n)
		}
	}
	variant := strength
	if pack > 0 {
		variant = strings.TrimSpace(variant + " " + strconv.Itoa(pack))
	}
	return &domain.ExtractedProduct{Name: name, Variant: variant, DosageForm: form, Strength: strength, PackSize: pack, Unit: "unit", Condition: condition, Quantity: quantity, Confidence: 0.78, EvidenceSpan: originalText}
}

func detectBrandAndModel(text string) (brand, model, fullName string) {
	lower := strings.ToLower(nlp.CleanSearchKey(text))

	// iPhone detection
	if strings.Contains(lower, "iphone") || strings.Contains(lower, "آیفون") || strings.Contains(lower, "ایفون") {
		brand = "Apple"
		if strings.Contains(lower, "15 pro max") || strings.Contains(lower, "15 پرو مکس") || strings.Contains(lower, "15 پرومکس") || strings.Contains(lower, "۱۵ پرو مکس") {
			model = "15 Pro Max"
		} else if strings.Contains(lower, "15 pro") || strings.Contains(lower, "15 پرو") || strings.Contains(lower, "۱۵ پرو") {
			model = "15 Pro"
		} else if strings.Contains(lower, "15 plus") || strings.Contains(lower, "15 پلاس") || strings.Contains(lower, "۱۵ پلاس") {
			model = "15 Plus"
		} else if strings.Contains(lower, "16 pro max") || strings.Contains(lower, "16 پرو مکس") || strings.Contains(lower, "۱۶ پرو مکس") {
			model = "16 Pro Max"
		} else if strings.Contains(lower, "16 pro") || strings.Contains(lower, "16 پرو") || strings.Contains(lower, "۱۶ پرو") {
			model = "16 Pro"
		} else if strings.Contains(lower, "16") || strings.Contains(lower, "۱۶") {
			model = "16"
		} else if strings.Contains(lower, "14 pro max") || strings.Contains(lower, "14 پرو مکس") || strings.Contains(lower, "۱۴ پرو مکس") {
			model = "14 Pro Max"
		} else if strings.Contains(lower, "14 pro") || strings.Contains(lower, "14 پرو") || strings.Contains(lower, "۱۴ پرو") {
			model = "14 Pro"
		} else if strings.Contains(lower, "14") || strings.Contains(lower, "۱۴") {
			model = "14"
		} else if strings.Contains(lower, "13 pro max") || strings.Contains(lower, "13 پرو مکس") || strings.Contains(lower, "۱۳ پرو مکس") {
			model = "13 Pro Max"
		} else if strings.Contains(lower, "13 pro") || strings.Contains(lower, "13 پرو") || strings.Contains(lower, "۱۳ پرو") {
			model = "13 Pro"
		} else if strings.Contains(lower, "13") || strings.Contains(lower, "۱۳") {
			model = "13"
		} else if strings.Contains(lower, "15") || strings.Contains(lower, "۱۵") {
			model = "15"
		} else {
			model = "iPhone"
		}
		fullName = "iPhone " + model
		return
	}

	// Samsung detection
	if strings.Contains(lower, "samsung") || strings.Contains(lower, "سامسونگ") {
		brand = "Samsung"
		if strings.Contains(lower, "s24 ultra") || strings.Contains(lower, "s24 اولترا") || strings.Contains(lower, "اس 24 اولترا") || strings.Contains(lower, "اس ۲۴ اولترا") || strings.Contains(lower, "s24ultra") {
			model = "S24 Ultra"
		} else if strings.Contains(lower, "s24 plus") || strings.Contains(lower, "s24 پلاس") || strings.Contains(lower, "اس 24 پلاس") || strings.Contains(lower, "اس ۲۴ پلاس") {
			model = "S24 Plus"
		} else if strings.Contains(lower, "s24") || strings.Contains(lower, "اس 24") || strings.Contains(lower, "اس ۲۴") {
			model = "S24"
		} else if strings.Contains(lower, "s23 ultra") || strings.Contains(lower, "s23 اولترا") || strings.Contains(lower, "اس 23 اولترا") || strings.Contains(lower, "اس ۲۳ اولترا") {
			model = "S23 Ultra"
		} else {
			model = "Galaxy"
		}
		fullName = "Samsung Galaxy " + model
		return
	}

	// MacBook detection
	if strings.Contains(lower, "macbook") || strings.Contains(lower, "مک بوک") || strings.Contains(lower, "مکبوک") {
		brand = "Apple"
		if strings.Contains(lower, "m3 pro") || strings.Contains(lower, "ام ۳ پرو") {
			model = "MacBook Pro M3 Pro"
		} else if strings.Contains(lower, "m3") || strings.Contains(lower, "ام ۳") {
			model = "MacBook Pro M3"
		} else if strings.Contains(lower, "m2") || strings.Contains(lower, "ام ۲") {
			model = "MacBook M2"
		} else {
			model = "MacBook"
		}
		fullName = model
		return
	}

	// PlayStation detection
	if strings.Contains(lower, "ps5") || strings.Contains(lower, "پلی استیشن") || strings.Contains(lower, "playstation") {
		brand = "Sony"
		if strings.Contains(lower, "slim") || strings.Contains(lower, "اسلیم") {
			model = "PS5 Slim"
		} else if strings.Contains(lower, "pro") || strings.Contains(lower, "پرو") {
			model = "PS5 Pro"
		} else {
			model = "PlayStation 5"
		}
		fullName = "Sony " + model
		return
	}

	return "", "", ""
}

func generateMessageID(chatID, msgID int64) string {
	return strconv.FormatInt(chatID, 10) + "_" + strconv.FormatInt(msgID, 10)
}
