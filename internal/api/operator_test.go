package api_test

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/api"
	"github.com/perso/perso-mind/internal/api/handlers"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOperatorTokenDoesNotTrustCallerIdentity(t *testing.T) {
	owner := uuid.New()
	token := strings.Repeat("x", 32)
	router := api.NewRouter(&api.Handlers{APIToken: token, OperatorUserID: owner, HealthHandler: handlers.NewHealthHandler(nil, nil, nil)})
	for _, value := range []string{"", "Bearer incorrect", "Bearer " + token} {
		req := httptest.NewRequest("GET", "/api/v1/session", nil)
		req.Header.Set("Authorization", value)
		req.Header.Set("X-User-ID", uuid.NewString())
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if value != "Bearer "+token {
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("invalid token accepted: %d", w.Code)
			}
			continue
		}
		var body struct {
			Data struct {
				UserID uuid.UUID `json:"user_id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Data.UserID != owner {
			t.Fatal("caller controlled operator identity")
		}
	}
}
