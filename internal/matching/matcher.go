package matching

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/nlp"
)

type MatchLevel string

const (
	MatchLevelExactID        MatchLevel = "exact_id"
	MatchLevelNormalizedName MatchLevel = "normalized_name"
	MatchLevelAlias          MatchLevel = "alias"
	MatchLevelBrandAndModel  MatchLevel = "brand_and_model"
	MatchLevelKeywords       MatchLevel = "keywords"
	MatchLevelSemantic       MatchLevel = "semantic"
)

type MatchResult struct {
	Matched        bool       `json:"matched"`
	ProductID      uuid.UUID  `json:"product_id"`
	ProductName    string     `json:"product_name"`
	Level          MatchLevel `json:"level"`
	Confidence     float64    `json:"confidence"`
	Reason         string     `json:"reason"`
	VariantMatched bool       `json:"variant_matched"`
}

type ProductCatalogReader interface {
	GetAllWithAliases(ctx context.Context) ([]domain.Product, error)
}

type Matcher struct {
	catalogReader ProductCatalogReader
}

func NewMatcher(catalogReader ProductCatalogReader) *Matcher {
	return &Matcher{catalogReader: catalogReader}
}

// MatchSellerProduct matches an extracted product against the database canonical products and aliases
func (m *Matcher) MatchSellerProduct(ctx context.Context, extracted *domain.ExtractedProduct) (*MatchResult, error) {
	if extracted == nil {
		return &MatchResult{Matched: false, Reason: "no product extracted"}, nil
	}

	products, err := m.catalogReader.GetAllWithAliases(ctx)
	if err != nil {
		return nil, err
	}

	normName := strings.ToLower(nlp.CleanSearchKey(extracted.Name))
	normBrand := strings.ToLower(nlp.CleanSearchKey(extracted.Brand))
	normModel := strings.ToLower(nlp.CleanSearchKey(extracted.Model))

	// Level 2: Exact Normalized Name match
	for _, p := range products {
		if !checkVariantMatch(extracted.Variant, p) {
			continue
		}
		pNorm := strings.ToLower(nlp.CleanSearchKey(p.Name))
		if pNorm != "" && (pNorm == normName || p.NormalizedName == normName) {
			return &MatchResult{
				Matched:        true,
				ProductID:      p.ID,
				ProductName:    p.Name,
				Level:          MatchLevelNormalizedName,
				Confidence:     0.98,
				Reason:         "Exact normalized name match: " + p.Name,
				VariantMatched: checkVariantMatch(extracted.Variant, p),
			}, nil
		}
	}

	// Level 4: Product Aliases match
	for _, p := range products {
		if !checkVariantMatch(extracted.Variant, p) {
			continue
		}
		for _, alias := range p.Aliases {
			normAlias := strings.ToLower(nlp.CleanSearchKey(alias.Alias))
			if normAlias != "" && (normAlias == normName) {
				return &MatchResult{
					Matched:        true,
					ProductID:      p.ID,
					ProductName:    p.Name,
					Level:          MatchLevelAlias,
					Confidence:     0.95,
					Reason:         "Product alias match on: " + alias.Alias,
					VariantMatched: checkVariantMatch(extracted.Variant, p),
				}, nil
			}
		}
	}

	// Level 3: Brand and Model Matching
	if normBrand != "" && normModel != "" {
		for _, p := range products {
			if !checkVariantMatch(extracted.Variant, p) {
				continue
			}
			pBrand := strings.ToLower(nlp.CleanSearchKey(p.Brand))
			pModel := strings.ToLower(nlp.CleanSearchKey(p.Model))

			if pBrand == normBrand && (pModel != "" && pModel == normModel) {
				return &MatchResult{
					Matched:        true,
					ProductID:      p.ID,
					ProductName:    p.Name,
					Level:          MatchLevelBrandAndModel,
					Confidence:     0.90,
					Reason:         "Brand and model matched: " + p.Brand + " " + p.Model,
					VariantMatched: checkVariantMatch(extracted.Variant, p),
				}, nil
			}
		}
	}

	// Level 5: Configurable keyword matching
	for _, p := range products {
		if !checkVariantMatch(extracted.Variant, p) {
			continue
		}
		keywords := strings.Fields(strings.ToLower(nlp.CleanSearchKey(p.Name)))
		if len(keywords) >= 2 {
			allFound := true
			for _, kw := range keywords {
				if !strings.Contains(normName, kw) {
					allFound = false
					break
				}
			}
			if allFound {
				return &MatchResult{
					Matched:        true,
					ProductID:      p.ID,
					ProductName:    p.Name,
					Level:          MatchLevelKeywords,
					Confidence:     0.82,
					Reason:         "Multi-keyword match for " + p.Name,
					VariantMatched: checkVariantMatch(extracted.Variant, p),
				}, nil
			}
		}
	}

	return &MatchResult{
		Matched:    false,
		Reason:     "No catalog product matched with sufficient confidence",
		Confidence: 0.0,
	}, nil
}

func checkVariantMatch(variant string, product domain.Product) bool {
	if variant == "" {
		return true // No variant specified
	}
	// An unconstrained catalog product has no conflicting variant. A catalog
	// that explicitly lists variants must contain the requested exact value.
	var attrs map[string]any
	if json.Unmarshal(product.Attributes, &attrs) != nil {
		return true
	}
	_, storage := attrs["storage"]
	_, variants := attrs["variants"]
	_, v := attrs["variant"]
	if !storage && !variants && !v {
		return true
	}
	return strings.Contains(strings.ToLower(string(product.Attributes)), "\""+strings.ToLower(variant)+"\"")
}
