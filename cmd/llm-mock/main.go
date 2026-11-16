package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/llm"
)

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	Stream         bool            `json:"stream"`
	Tools          json.RawMessage `json:"tools"`
	ResponseFormat *struct {
		Type string `json:"type"`
	} `json:"response_format"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type errorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func main() {
	addr := env("LLM_MOCK_ADDR", "0.0.0.0:8091")
	h := newHandler(env("LLM_MOCK_TOKEN", ""), env("ENVIRONMENT", "development"))
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	slog.Info("starting OpenAI-compatible mock", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("mock server stopped", "error", err)
		os.Exit(1)
	}
}

func newHandler(token, environment string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request")
			return
		}
		if !authorized(r, token) {
			writeError(w, http.StatusUnauthorized, "invalid mock token", "authentication_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": []map[string]any{{"id": "perso-healthcare-mock", "object": "model", "owned_by": "perso"}}})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request")
			return
		}
		if !authorized(r, token) {
			writeError(w, http.StatusUnauthorized, "invalid mock token", "authentication_error")
			return
		}
		var req chatRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		if err := decoder.Decode(&req); err != nil || len(req.Messages) == 0 {
			writeError(w, http.StatusBadRequest, "model and messages are required", "invalid_request")
			return
		}
		if req.Model != "perso-healthcare-mock" {
			writeError(w, http.StatusBadRequest, "unsupported model", "invalid_request")
			return
		}
		if req.Stream || len(req.Tools) > 0 {
			writeError(w, http.StatusBadRequest, "streaming and tools are unsupported", "unsupported_feature")
			return
		}
		if req.ResponseFormat != nil && req.ResponseFormat.Type != "json_object" {
			writeError(w, http.StatusBadRequest, "only json_object response format is supported", "unsupported_feature")
			return
		}
		scenario := env("LLM_MOCK_SCENARIO", "")
		if scenario != "" && environment != "development" && environment != "test" {
			writeError(w, http.StatusForbidden, "failure scenarios are disabled outside development and test", "policy_error")
			return
		}
		switch scenario {
		case "rate_limit":
			writeError(w, http.StatusTooManyRequests, "simulated rate limit", "rate_limit_error")
			return
		case "server_error":
			writeError(w, http.StatusBadGateway, "simulated provider failure", "server_error")
			return
		case "malformed":
			writeJSON(w, http.StatusOK, map[string]any{"id": "mock-malformed", "object": "chat.completion", "choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "not-json"}}}})
			return
		case "timeout":
			time.Sleep(30 * time.Second)
			return
		}
		content := responseContent(req.Messages)
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      "chatcmpl-perso-mock",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   req.Model,
			"choices": []map[string]any{{"index": 0, "message": map[string]string{"role": "assistant", "content": content}, "finish_reason": "stop"}},
			"usage":   map[string]int{"prompt_tokens": 64, "completion_tokens": 48, "total_tokens": 112},
		})
	})
	return mux
}

func responseContent(messages []chatMessage) string {
	user := ""
	system := ""
	for _, message := range messages {
		if message.Role == "user" {
			user = message.Content
		}
		if message.Role == "system" {
			system = message.Content
		}
	}
	if strings.Contains(system, "Output only the question") || strings.Contains(system, "Generate one concise Persian question") {
		return "لطفاً قیمت و وضعیت این کالا را اعلام می‌کنید؟"
	}
	result := fixtureExtraction(user)
	encoded, _ := json.Marshal(result)
	return string(encoded)
}

func fixtureExtraction(input string) llm.LLMExtractionResponse {
	if strings.Contains(input, "سرم نرمال سالین") {
		return llm.LLMExtractionResponse{
			Classification: domain.ClassificationSeller,
			Confidence:     0.94,
			Products:       []llm.LLMExtractedProduct{{Name: "سرم نرمال سالین", Brand: "داروسازی ایرانی", Manufacturer: "داروسازی ایرانی", Variant: "500ml", Condition: domain.ConditionNew, Quantity: 12, Confidence: 0.91, EvidenceSpan: "سرم نرمال سالین"}},
			Prices:         []llm.LLMExtractedPrice{{Amount: 25000, Currency: domain.CurrencyToman, Raw: "۲۵۰۰۰ تومان", Confidence: 0.9, EvidenceSpan: "۲۵۰۰۰ تومان"}},
			Location:       &llm.LLMExtractedLocation{City: "تهران", Area: "پاستور", LocationText: "پاستور", Confidence: 0.85, EvidenceSpan: "پاستور"},
			TokenUsage:     llm.TokenUsage{PromptTokens: 64, CompletionTokens: 48, TotalTokens: 112},
		}
	}
	if strings.Contains(input, "آیفون") || strings.Contains(strings.ToLower(input), "iphone") {
		return llm.LLMExtractionResponse{
			Classification: domain.ClassificationSeller,
			Confidence:     0.9,
			Products:       []llm.LLMExtractedProduct{{Name: "iPhone 15 Pro", Brand: "Apple", Model: "15 Pro", Condition: domain.ConditionUsedGood, Quantity: 1, Confidence: 0.9, EvidenceSpan: firstEvidence(input, "آیفون", "iPhone")}},
			MissingFields:  []domain.MissingField{domain.FieldPrice, domain.FieldCity},
			TokenUsage:     llm.TokenUsage{PromptTokens: 64, CompletionTokens: 48, TotalTokens: 112},
		}
	}
	return llm.LLMExtractionResponse{Classification: domain.ClassificationUncertain, Confidence: 0.2, MissingFields: []domain.MissingField{domain.FieldPrice, domain.FieldCity}, TokenUsage: llm.TokenUsage{PromptTokens: 64, CompletionTokens: 48, TotalTokens: 112}}
}

func firstEvidence(input string, options ...string) string {
	for _, option := range options {
		if strings.Contains(input, option) {
			return option
		}
	}
	return options[0]
}

func authorized(r *http.Request, token string) bool {
	if token == "" {
		return true
	}
	return r.Header.Get("Authorization") == "Bearer "+token
}

func writeError(w http.ResponseWriter, status int, message, kind string) {
	writeJSON(w, status, errorResponse{Error: struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	}{Message: message, Type: kind}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
