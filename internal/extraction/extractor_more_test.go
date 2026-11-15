package extraction_test

import (
	"testing"

	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/extraction"
	"github.com/perso/perso-mind/internal/nlp"
)

// TestExtractProducts_CoverageGaps targets the branches of ExtractProducts,
// extractLocation, extractProductDetails and detectBrandAndModel that the
// original end-to-end fixtures never reach: empty multi-line entries,
// single-line splitting, the TB storage variant, quantity parsing,
// condition fallbacks, and every brand/model tier.
func TestExtractProducts_CoverageGaps(t *testing.T) {
	extractor := extraction.NewExtractor()

	t.Run("multi-line message keeps only product lines and dedupes repeats", func(t *testing.T) {
		// A blank line must be skipped, "خط مقدم" is a non-product line
		// (no brand), and the repeated iPhone line must be deduplicated by
		// name+variant.
		text := "فروش ویژه\nآیفون ۱۵ پرو ۲۵۶ گیگ در حد نو\n\nخط مقدم\nآیفون ۱۵ پرو ۲۵۶ گیگ در حد نو"
		products := extractor.ExtractProducts(nlp.Normalize(text), text)
		if len(products) != 1 {
			t.Fatalf("products = %+v, want exactly 1 deduped product", products)
		}
		p := products[0]
		if p.Brand != "Apple" || p.Model != "15 Pro" || p.Variant != "256GB" {
			t.Errorf("product = %+v, want Apple iPhone 15 Pro 256GB", p)
		}
	})

	t.Run("single line is split by conjunctions and short segments skipped", func(t *testing.T) {
		// "و" separates two products; "ا" is a segment under 4 bytes that
		// must be skipped by the length guard.
		text := "آیفون ۱۵ پرو ۲۵۶ گیگ و سامسونگ اس ۲۴ اولترا ۵۱۲ گیگ و ا"
		products := extractor.ExtractProducts(nlp.Normalize(text), text)
		if len(products) != 2 {
			t.Fatalf("products = %+v, want 2", products)
		}
		if products[0].Brand != "Apple" || products[0].Model != "15 Pro" {
			t.Errorf("products[0] = %+v, want Apple 15 Pro", products[0])
		}
		if products[1].Brand != "Samsung" || products[1].Model != "S24 Ultra" {
			t.Errorf("products[1] = %+v, want Samsung S24 Ultra", products[1])
		}
	})

	t.Run("conjunction split dedupes identical segments", func(t *testing.T) {
		text := "آیفون ۱۵ پرو ۱۲۸ گیگ و آیفون ۱۵ پرو ۱۲۸ گیگ"
		products := extractor.ExtractProducts(nlp.Normalize(text), text)
		if len(products) != 1 {
			t.Fatalf("products = %+v, want 1 deduped", products)
		}
	})

	t.Run("fallback extracts a single product from the whole text", func(t *testing.T) {
		text := "مک بوک ۱۶ گیگ رم"
		products := extractor.ExtractProducts(nlp.Normalize(text), text)
		if len(products) != 1 {
			t.Fatalf("products = %+v, want 1", products)
		}
		if products[0].Brand != "Apple" || products[0].Model != "MacBook" {
			t.Errorf("product = %+v, want Apple MacBook", products[0])
		}
	})
}

func TestExtractProductDetails_Branches(t *testing.T) {
	extractor := extraction.NewExtractor()

	run := func(t *testing.T, text string) domain.ExtractedProduct {
		t.Helper()
		norm := nlp.Normalize(text)
		products := extractor.ExtractProducts(norm, text)
		if len(products) != 1 {
			t.Fatalf("products = %+v, want exactly 1 for input %q", products, text)
		}
		return products[0]
	}

	t.Run("TB storage variant", func(t *testing.T) {
		p := run(t, "آیفون ۱۵ پرو ۲ ترابایت")
		if p.Variant != "2TB" {
			t.Errorf("Variant = %q, want 2TB", p.Variant)
		}
	})

	t.Run("used good condition from battery health", func(t *testing.T) {
		p := run(t, "آیفون ۱۵ سلامت باتری ۹۵ درصد")
		if p.Condition != domain.ConditionUsedGood {
			t.Errorf("Condition = %v, want used_good", p.Condition)
		}
	})

	t.Run("used poor condition is detected", func(t *testing.T) {
		// "تعمیر شده" (repaired) triggers used_poor without the Persian
		// conjunction " و " that would otherwise split the segment.
		p := run(t, "آیفون ۱۴ تعمیر شده")
		if p.Condition != domain.ConditionUsedPoor {
			t.Errorf("Condition = %v, want used_poor", p.Condition)
		}
	})

	t.Run("quantity parsed from تعداد", func(t *testing.T) {
		p := run(t, "آیفون ۱۳ تعداد ۳ عدد")
		if p.Quantity != 3 {
			t.Errorf("Quantity = %d, want 3", p.Quantity)
		}
	})

	t.Run("out-of-range quantity falls back to 1", func(t *testing.T) {
		p := run(t, "آیفون ۱۳ تعداد ۱۵۰۰ عدد")
		if p.Quantity != 1 {
			t.Errorf("Quantity = %d, want 1", p.Quantity)
		}
	})
}

func TestDetectBrandAndModel_AllTiers(t *testing.T) {
	extractor := extraction.NewExtractor()

	run := func(t *testing.T, text string) (brand, model, name string) {
		t.Helper()
		products := extractor.ExtractProducts(nlp.Normalize(text), text)
		if len(products) != 1 {
			t.Fatalf("products = %+v, want exactly 1 for input %q", products, text)
		}
		return products[0].Brand, products[0].Model, products[0].Name
	}

	cases := []struct {
		text      string
		wantBrand string
		wantModel string
	}{
		// iPhone tiers
		{"آیفون ۱۵ پرو مکس ۲۵۶ گیگ", "Apple", "15 Pro Max"},
		{"آیفون ۱۵ پلاس", "Apple", "15 Plus"},
		{"آیفون ۱۶ پرو مکس", "Apple", "16 Pro Max"},
		{"آیفون ۱۶ پرو", "Apple", "16 Pro"},
		{"آیفون ۱۶", "Apple", "16"},
		{"آیفون ۱۴ پرو مکس", "Apple", "14 Pro Max"},
		{"آیفون ۱۴ پرو", "Apple", "14 Pro"},
		{"آیفون ۱۴", "Apple", "14"},
		{"آیفون ۱۳ پرو مکس", "Apple", "13 Pro Max"},
		{"آیفون ۱۳ پرو", "Apple", "13 Pro"},
		{"آیفون ۱۳", "Apple", "13"},
		{"آیفون ۱۵", "Apple", "15"},
		{"آیفون دست دوم", "Apple", "iPhone"},
		// Samsung tiers
		{"سامسونگ s24 اولترا", "Samsung", "S24 Ultra"},
		{"سامسونگ s24 پلاس", "Samsung", "S24 Plus"},
		{"سامسونگ s24", "Samsung", "S24"},
		{"سامسونگ s23 اولترا", "Samsung", "S23 Ultra"},
		{"سامسونگ", "Samsung", "Galaxy"},
		// MacBook tiers (chip model must appear as Latin "m2"/"m3" for the
		// production heuristics to fire)
		{"مک بوک m3 pro", "Apple", "MacBook Pro M3 Pro"},
		{"مک بوک m3", "Apple", "MacBook Pro M3"},
		{"مک بوک m2", "Apple", "MacBook M2"},
		{"مک بوک", "Apple", "MacBook"},
		// PlayStation tiers
		{"پلی استیشن ۵ اسلیم", "Sony", "PS5 Slim"},
		{"پلی استیشن ۵ پرو", "Sony", "PS5 Pro"},
		{"پلی استیشن ۵", "Sony", "PlayStation 5"},
	}

	for _, tt := range cases {
		t.Run(tt.wantModel, func(t *testing.T) {
			brand, model, name := run(t, tt.text)
			if brand != tt.wantBrand {
				t.Errorf("Brand = %q, want %q", brand, tt.wantBrand)
			}
			if model != tt.wantModel {
				t.Errorf("Model = %q, want %q", model, tt.wantModel)
			}
			if name == "" {
				t.Errorf("Name is empty for input %q", tt.text)
			}
		})
	}

	t.Run("unknown brand yields no product", func(t *testing.T) {
		products := extractor.ExtractProducts(nlp.Normalize("تلویزیون ۵۰ اینچ"), "تلویزیون ۵۰ اینچ")
		if len(products) != 0 {
			t.Fatalf("products = %+v, want none", products)
		}
	})
}

func TestExtractLocation_Branches(t *testing.T) {
	extractor := extraction.NewExtractor()

	t.Run("city without a known Tehran area", func(t *testing.T) {
		text := "فروش آیفون ۱۵، تحویل در مشهد"
		loc := extractor.ExtractFromMessage(&domain.TelegramMessage{MessageText: text}).Location
		if loc == nil {
			t.Fatalf("expected a location")
		}
		if loc.City != "مشهد" || loc.Area != "" || loc.EvidenceSpan != "مشهد" {
			t.Errorf("location = %+v, want city مشهد only", loc)
		}
	})

	t.Run("no location yields nil", func(t *testing.T) {
		text := "فروش آیفون ۱۵ بدون ذکر مکان"
		loc := extractor.ExtractFromMessage(&domain.TelegramMessage{MessageText: text}).Location
		if loc != nil {
			t.Fatalf("location = %+v, want nil", loc)
		}
	})

	t.Run("Tehran area sets city تهران", func(t *testing.T) {
		text := "فروش آیفون ۱۵، تحویل سعادت آباد"
		loc := extractor.ExtractFromMessage(&domain.TelegramMessage{MessageText: text}).Location
		if loc == nil {
			t.Fatalf("expected a location")
		}
		if loc.City != "تهران" || loc.Area != "سعادت آباد" || loc.EvidenceSpan != "سعادت آباد" {
			t.Errorf("location = %+v, want تهران / سعادت آباد", loc)
		}
	})
}
