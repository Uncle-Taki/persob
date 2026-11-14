package handlers_test

import (
	"github.com/perso/perso-mind/internal/api/handlers"
	"github.com/perso/perso-mind/internal/telegram"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSourceCreationRejectsUnverifiedMetadata(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
	}{
		{"peer metadata without address", `{"telegram_peer_id":-100777,"source_type":"forum","access_type":"private","capabilities":{"can_read_history":true}}`, http.StatusBadRequest},
		{"detected type mismatch", `{"address":"@tech_forum","expected_type":"broadcast_channel"}`, http.StatusConflict},
		{"invalid expected type", `{"address":"@tech_forum","expected_type":"administrator"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("unverified request reached persistence (panic): %v", p)
				}
			}()
			h := handlers.NewTelegramSourceHandler(nil, telegram.NewResolver(nil, true))
			w := httptest.NewRecorder()
			h.Create(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body)))
			if w.Code != tc.status {
				t.Errorf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
		})
	}
}
