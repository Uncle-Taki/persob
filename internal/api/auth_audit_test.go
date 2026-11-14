package api_test

import (
	"github.com/perso/perso-mind/internal/api"
	"github.com/perso/perso-mind/internal/api/handlers"
	"github.com/perso/perso-mind/internal/telegram"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIRejectsAnonymousAndSpoofedIdentityByDefault(t *testing.T) {
	h := &api.Handlers{
		HealthHandler:         handlers.NewHealthHandler(nil, nil, nil),
		InterestHandler:       handlers.NewInterestHandler(nil, nil),
		WatchSourceHandler:    handlers.NewWatchSourceHandler(nil),
		TelegramSourceHandler: handlers.NewTelegramSourceHandler(nil, telegram.NewResolver(nil, true)),
		ProductHandler:        handlers.NewProductHandler(nil, nil), OfferHandler: handlers.NewOfferHandler(nil, nil, nil),
		ConversationHandler: handlers.NewConversationHandler(nil, nil, nil), AnalyticsHandler: handlers.NewAnalyticsHandler(nil, nil, nil, nil, nil),
	}
	router := api.NewRouter(h)
	for _, user := range []string{"", "22222222-2222-2222-2222-222222222222"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sources/resolve", strings.NewReader(`{"address":"@tech_forum"}`))
		req.Header.Set("X-User-ID", user)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("untrusted identity %q accepted: status=%d", user, w.Code)
		}
	}
}
