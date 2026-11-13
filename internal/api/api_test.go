package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/perso/perso-mind/internal/api"
	"github.com/perso/perso-mind/internal/api/handlers"
)

func TestHealthEndpoints(t *testing.T) {
	healthH := handlers.NewHealthHandler(nil, nil, nil)
	router := api.NewRouter(&api.Handlers{
		HealthHandler:       healthH,
		InterestHandler:     handlers.NewInterestHandler(nil, nil),
		WatchSourceHandler:  handlers.NewWatchSourceHandler(nil),
		ProductHandler:      handlers.NewProductHandler(nil, nil),
		OfferHandler:        handlers.NewOfferHandler(nil, nil),
		ConversationHandler: handlers.NewConversationHandler(nil, nil, nil),
		AnalyticsHandler:    handlers.NewAnalyticsHandler(nil, nil, nil, nil, nil),
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
