package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/perso/perso-mind/internal/api"
	"github.com/perso/perso-mind/internal/api/handlers"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/telegram"
)

func TestHealthEndpoints(t *testing.T) {
	healthH := handlers.NewHealthHandler(nil, nil, nil)
	resolver := telegram.NewResolver(nil, true)
	sourceH := handlers.NewTelegramSourceHandler(nil, resolver)

	router := api.NewRouter(&api.Handlers{
		HealthHandler:         healthH,
		InterestHandler:       handlers.NewInterestHandler(nil, nil),
		WatchSourceHandler:    handlers.NewWatchSourceHandler(nil),
		TelegramSourceHandler: sourceH,
		ProductHandler:        handlers.NewProductHandler(nil, nil),
		OfferHandler:          handlers.NewOfferHandler(nil, nil, nil),
		ConversationHandler:   handlers.NewConversationHandler(nil, nil, nil),
		AnalyticsHandler:      handlers.NewAnalyticsHandler(nil, nil, nil, nil, nil),
	})

	// 1. Test /health
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("/health status = %d, want %d", w.Code, http.StatusOK)
	}

	// 2. Test /openapi.yaml
	reqOas := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	wOas := httptest.NewRecorder()
	router.ServeHTTP(wOas, reqOas)

	if wOas.Code != http.StatusOK {
		t.Errorf("/openapi.yaml status = %d, want %d", wOas.Code, http.StatusOK)
	}

	if wOas.Header().Get("Content-Type") != "application/yaml" {
		t.Errorf("Content-Type = %s, want application/yaml", wOas.Header().Get("Content-Type"))
	}
}

func TestResolveSourceEndpoint(t *testing.T) {
	resolver := telegram.NewResolver(nil, true)
	sourceH := handlers.NewTelegramSourceHandler(nil, resolver)

	router := api.NewRouter(&api.Handlers{
		HealthHandler:         handlers.NewHealthHandler(nil, nil, nil),
		InterestHandler:       handlers.NewInterestHandler(nil, nil),
		WatchSourceHandler:    handlers.NewWatchSourceHandler(nil),
		TelegramSourceHandler: sourceH,
		ProductHandler:        handlers.NewProductHandler(nil, nil),
		OfferHandler:          handlers.NewOfferHandler(nil, nil, nil),
		ConversationHandler:   handlers.NewConversationHandler(nil, nil, nil),
		AnalyticsHandler:      handlers.NewAnalyticsHandler(nil, nil, nil, nil, nil),
	})

	// Test resolving forum entity
	body, _ := json.Marshal(domain.ResolveSourceRequest{
		Address:      "https://t.me/tech_forum_iran",
		ExpectedType: domain.TelegramSourceTypeForum,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sources/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("resolve endpoint status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var apiResp struct {
		Success bool                         `json:"success"`
		Data    domain.ResolveSourceResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &apiResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	resp := apiResp.Data

	if resp.DetectedType != domain.TelegramSourceTypeForum {
		t.Errorf("DetectedType = %s, want forum", resp.DetectedType)
	}
	if !resp.Capabilities.HasTopics {
		t.Errorf("Expected Capabilities.HasTopics = true for forum")
	}
	if !resp.AccountHasAccess {
		t.Errorf("Expected AccountHasAccess = true")
	}
}
