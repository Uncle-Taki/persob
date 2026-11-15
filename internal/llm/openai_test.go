package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/perso/perso-mind/internal/domain"
)

func TestOpenAICompatibleExtractionAndAuth(t *testing.T) {
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		var request openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Model != "fixture" || request.ResponseFormat == nil || request.ResponseFormat.Type != "json_object" {
			t.Fatalf("unexpected request: %+v", request)
		}
		content := `{"classification":"seller","confidence":0.9,"products":[{"name":"سرم","condition":"new","quantity":1,"confidence":0.8,"evidence_span":"سرم"}],"prices":[],"missing_fields":["price"]}`
		writeTestJSON(w, map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": content}}}, "usage": map[string]int{"prompt_tokens": 4, "completion_tokens": 5, "total_tokens": 9}})
	}))
	defer server.Close()

	provider := NewOpenAICompatibleProvider(server.URL+"/v1", "fixture", "secret", 2, 1)
	response, err := provider.ExtractMarketplaceInfo(context.Background(), "فروش سرم")
	if err != nil {
		t.Fatalf("extraction failed: %v", err)
	}
	if gotPath != "/v1/chat/completions" || gotAuth != "Bearer secret" || response.TokenUsage.TotalTokens != 9 {
		t.Fatalf("request/usage mismatch path=%q auth=%q usage=%+v", gotPath, gotAuth, response.TokenUsage)
	}
}

func TestOpenAICompatibleRetriesRateLimitAndQuestionFallback(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"try later"}}`))
			return
		}
		writeTestJSON(w, map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": "لطفاً قیمت را اعلام می‌کنید؟"}}}})
	}))
	defer server.Close()
	provider := NewOpenAICompatibleProvider(server.URL, "fixture", "", 2, 2)
	question, err := provider.GenerateConversationQuestion(context.Background(), "سرم", domain.FieldPrice, nil)
	if err != nil || question == "" || calls != 2 {
		t.Fatalf("question=%q err=%v calls=%d", question, err, calls)
	}
}

func TestOpenAICompatibleMalformedAndContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": "not-json"}}}})
	}))
	defer server.Close()
	provider := NewOpenAICompatibleProvider(server.URL, "fixture", "", 2, 1)
	if _, err := provider.ExtractMarketplaceInfo(context.Background(), "فروش سرم"); err == nil || !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("malformed response error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.ExtractMarketplaceInfo(ctx, "فروش سرم"); err == nil {
		t.Fatal("canceled request succeeded")
	}
}

func writeTestJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
