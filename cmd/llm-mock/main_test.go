package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/perso/perso-mind/internal/llm"
)

func TestHealthcareFixtureThroughHTTPAdapter(t *testing.T) {
	server := httptest.NewServer(newHandler("private-fixture-token", "test"))
	defer server.Close()
	provider := llm.NewOpenAICompatibleProvider(server.URL+"/v1", "perso-healthcare-mock", "private-fixture-token", 2, 1)
	result, err := provider.ExtractMarketplaceInfo(context.Background(), "فروش سرم نرمال سالین 500ml تعداد ۱۲ عدد، ۲۵۰۰۰ تومان، تهران پاستور")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Products) != 1 || result.Products[0].Name != "سرم نرمال سالین" || len(result.Prices) != 1 || result.Prices[0].Amount != 25000 {
		t.Fatalf("unexpected fixture: %+v", result)
	}
	response, err := http.Get(server.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal("mock accepted missing credentials")
	}
}
