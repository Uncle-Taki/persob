package analytics_test

import (
	"testing"

	"github.com/perso/perso-mind/internal/domain"
)

func TestPriceComparisonsStructures(t *testing.T) {
	condComp := domain.ConditionComparison{
		Condition: domain.ConditionLikeNew,
		AvgPrice:  95000000,
		MinPrice:  90000000,
		MaxPrice:  105000000,
		Count:     12,
	}

	if condComp.AvgPrice < condComp.MinPrice || condComp.AvgPrice > condComp.MaxPrice {
		t.Errorf("Average price out of min/max bounds")
	}

	cityComp := domain.CityComparison{
		City:     "تهران",
		AvgPrice: 94000000,
		MinPrice: 88000000,
		MaxPrice: 102000000,
		Count:    45,
	}

	if cityComp.Count <= 0 {
		t.Errorf("City comparison count must be positive")
	}

	brandComp := domain.BrandComparison{
		Brand:    "Apple",
		AvgPrice: 85000000,
		MinPrice: 40000000,
		MaxPrice: 140000000,
		Count:    80,
	}

	if brandComp.Brand != "Apple" {
		t.Errorf("Brand mismatch")
	}
}
