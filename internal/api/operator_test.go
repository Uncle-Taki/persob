package api_test

import (
	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/api"
	"github.com/perso/perso-mind/internal/api/handlers"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLegacyOperatorTokenCannotEstablishBrowserIdentity(t *testing.T) {
	owner := uuid.New()
	token := strings.Repeat("x", 32)
	router := api.NewRouter(&api.Handlers{APIToken: token, OperatorUserID: owner, HealthHandler: handlers.NewHealthHandler(nil, nil, nil)})
	for _, value := range []string{"", "Bearer incorrect", "Bearer " + token} {
		req := httptest.NewRequest("GET", "/api/v1/session", nil)
		req.Header.Set("Authorization", value)
		req.Header.Set("X-User-ID", uuid.NewString())
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("legacy token established identity: %d", w.Code)
		}
	}
}
