package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/perso/perso-mind/internal/domain"
)

// coverTransport redirects the hardcoded Gemini endpoint to a test server.
type coverTransport struct {
	Target    string
	Calls     *int32
	forwarder *http.Client
}

// RoundTrip replays the request against the httptest server, because the
// production endpoint is hardcoded to generativelanguage.googleapis.com.
func (ct coverTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if ct.Calls != nil {
		atomic.AddInt32(ct.Calls, 1)
	}
	req := r.Clone(r.Context())
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(ct.Target, "http://")
	req.URL.Path = "/v1beta/models/redirected:generateContent"
	req.URL.RawQuery = ""
	return ct.forwarder.Do(req)
}

// geminiCoverServer serves canned Gemini responses, one per handler entry.
func geminiCoverServer(t *testing.T, handler func(call int, r *http.Request) (int, string)) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status, body := handler(int(atomic.AddInt32(&calls, 1)), r)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func newCoveredGemini(t *testing.T, srv *httptest.Server, maxRetries int) (*GeminiProvider, *int32) {
	t.Helper()
	p := NewGeminiProvider("test-key", "redirected", 5, maxRetries)
	var calls int32
	p.httpClient.Transport = coverTransport{Target: srv.URL, Calls: &calls, forwarder: &http.Client{}}
	return p, &calls
}

// validGeminiBody is a 200 response whose model text is a well-formed but
// empty extraction: unmarshal succeeds, evidence validation must fail.
const validGeminiBody = `{"candidates":[{"content":{"parts":[{"text":"{}"}]}}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`

func TestGeminiExtractMarketplaceInfo(t *testing.T) {
	t.Run("empty api key", func(t *testing.T) {
		p := NewGeminiProvider("", "redirected", 1, 1)
		_, err := p.ExtractMarketplaceInfo(context.Background(), "فروش آیفون")
		if err == nil || err.Error() != "gemini api key is empty" {
			t.Fatalf("err = %v, want empty api key error", err)
		}
	})

	t.Run("success with evidence", func(t *testing.T) {
		srv, _ := geminiCoverServer(t, func(call int, r *http.Request) (int, string) {
			if r.Header.Get("x-goog-api-key") != "test-key" {
				t.Errorf("missing x-goog-api-key header")
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("missing content type")
			}
			var parsed struct {
				Contents []struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"contents"`
			}
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Contents) == 0 {
				t.Errorf("request body is not the expected gemini request: %s", string(raw))
			} else {
				sent := parsed.Contents[0].Parts[0].Text
				if !strings.Contains(sent, "فروش آیفون") || !strings.Contains(sent, "<untrusted_marketplace_message>") {
					t.Errorf("user part missing sanitized message: %s", sent)
				}
			}
			return http.StatusOK, validGeminiBody
		})
		p, calls := newCoveredGemini(t, srv, 3)

		// A response with no usable evidence must be rejected by ValidateLLMEvidence.
		_, err := p.ExtractMarketplaceInfo(context.Background(), "فروش آیفون")
		if err == nil || !strings.Contains(err.Error(), "validate llm output") {
			t.Fatalf("expected validation failure for empty extraction, got %v", err)
		}

		srv2, _ := geminiCoverServer(t, func(call int, r *http.Request) (int, string) {
			// The model output itself is JSON, embedded as a JSON string.
			return http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"{\"classification\": \"seller\", \"confidence\": 0.9, \"products\": [{\"name\": \"آیفون 15\", \"brand\": \"Apple\", \"model\": \"15\", \"variant\": \"256GB\", \"condition\": \"used_good\", \"quantity\": 1, \"confidence\": 0.9, \"evidence_span\": \"آیفون\"}], \"prices\": [{\"amount\": 98000000, \"currency\": \"IRR_TOMAN\", \"raw\": \"۹۸ میلیون\", \"negotiable\": false, \"confidence\": 0.9, \"evidence_span\": \"آیفون\"}], \"location\": {\"city\": \"تهران\", \"area\": \"علاءالدین\", \"location_text\": \"تهران\", \"confidence\": 0.8, \"evidence_span\": \"آیفون\"}, \"missing_fields\": [\"price\"]}"}]}}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":4,"totalTokenCount":11}}`
		})
		p2, _ := newCoveredGemini(t, srv2, 3)

		resp, err := p2.ExtractMarketplaceInfo(context.Background(), "فروش آیفون")
		if err != nil {
			t.Fatalf("ExtractMarketplaceInfo() error = %v", err)
		}
		if resp.Classification != domain.ClassificationSeller || resp.Confidence != 0.9 {
			t.Errorf("resp = %+v", resp)
		}
		if resp.TokenUsage != (TokenUsage{PromptTokens: 7, CompletionTokens: 4, TotalTokens: 11}) {
			t.Errorf("TokenUsage = %+v", resp.TokenUsage)
		}
		if len(resp.Products) != 1 || resp.Products[0].Name != "آیفون 15" {
			t.Errorf("Products = %+v", resp.Products)
		}
		if len(resp.Prices) != 1 || resp.Prices[0].Amount != 98000000 {
			t.Errorf("Prices = %+v", resp.Prices)
		}
		if resp.Location == nil || resp.Location.City != "تهران" {
			t.Errorf("Location = %+v", resp.Location)
		}
		_ = calls
	})

	t.Run("http transport error exhausts retries", func(t *testing.T) {
		p := NewGeminiProvider("test-key", "redirected", 5, 2)
		// Unreachable endpoint: the forwarded request always fails.
		p.httpClient.Transport = coverTransport{Target: "http://127.0.0.1:1", Calls: new(int32), forwarder: &http.Client{}}
		start := time.Now()
		_, err := p.ExtractMarketplaceInfo(context.Background(), "فروش آیفون")
		elapsed := time.Since(start)
		if err == nil || !strings.Contains(err.Error(), "gemini api call failed") || !strings.Contains(err.Error(), "exceeded max retries") {
			t.Fatalf("err = %v, want max retries exhaustion", err)
		}
		// Backoff schedule is 500ms then 1000ms between the two failed attempts.
		if elapsed < time.Second || elapsed > 5*time.Second {
			t.Errorf("elapsed %v, want ~1.5s backoff (not too slow, not skipping retries)", elapsed)
		}
	})

	t.Run("429 then success exercises retry", func(t *testing.T) {
		srv, _ := geminiCoverServer(t, func(call int, r *http.Request) (int, string) {
			if call == 1 {
				return http.StatusTooManyRequests, `{"error":{"code":429,"message":"rate limited","status":"RESOURCE_EXHAUSTED"}}`
			}
			return http.StatusOK, validGeminiBody
		})
		p, calls := newCoveredGemini(t, srv, 3)

		_, err := p.ExtractMarketplaceInfo(context.Background(), "فروش آیفون")
		if err == nil || !strings.Contains(err.Error(), "validate llm output") {
			t.Fatalf("expected validation failure after retry, got %v", err)
		}
		if got := *calls; got != 2 {
			t.Errorf("server calls = %d, want 2 (429 retried then success)", got)
		}
	})

	t.Run("500 exhausts retries", func(t *testing.T) {
		srv, calls := geminiCoverServer(t, func(call int, r *http.Request) (int, string) {
			return http.StatusInternalServerError, `{"error":{"code":500,"message":"boom","status":"INTERNAL"}}`
		})
		p, _ := newCoveredGemini(t, srv, 3)

		_, err := p.ExtractMarketplaceInfo(context.Background(), "فروش آیفون")
		if err == nil || !strings.Contains(err.Error(), "exceeded max retries") || !strings.Contains(err.Error(), "status 500") {
			t.Fatalf("err = %v, want retry exhaustion with status 500", err)
		}
		if got := *calls; got != 3 {
			t.Errorf("server calls = %d, want 3", got)
		}
	})

	t.Run("non-retryable 400 fails immediately", func(t *testing.T) {
		srv, calls := geminiCoverServer(t, func(call int, r *http.Request) (int, string) {
			return http.StatusBadRequest, `{"error":{"code":400,"message":"bad request","status":"INVALID_ARGUMENT"}}`
		})
		p, _ := newCoveredGemini(t, srv, 3)

		_, err := p.ExtractMarketplaceInfo(context.Background(), "فروش آیفون")
		if err == nil || !strings.Contains(err.Error(), "status 400") {
			t.Fatalf("err = %v, want immediate status 400 failure", err)
		}
		if got := *calls; got != 1 {
			t.Errorf("server calls = %d, want 1 (400 must not retry)", got)
		}
	})

	t.Run("200 with embedded gemini error", func(t *testing.T) {
		srv, _ := geminiCoverServer(t, func(call int, r *http.Request) (int, string) {
			return http.StatusOK, `{"error":{"code":400,"message":"blocked content","status":"INVALID_ARGUMENT"}}`
		})
		p, _ := newCoveredGemini(t, srv, 3)

		_, err := p.ExtractMarketplaceInfo(context.Background(), "فروش آیفون")
		if err == nil || !strings.Contains(err.Error(), "gemini error (400): blocked content") {
			t.Fatalf("err = %v, want embedded gemini error", err)
		}
	})

	t.Run("200 with empty candidates", func(t *testing.T) {
		srv, _ := geminiCoverServer(t, func(call int, r *http.Request) (int, string) {
			return http.StatusOK, `{"candidates":[]}`
		})
		p, _ := newCoveredGemini(t, srv, 3)

		_, err := p.ExtractMarketplaceInfo(context.Background(), "فروش آیفون")
		if err == nil || !strings.Contains(err.Error(), "empty candidate content") {
			t.Fatalf("err = %v, want empty candidate error", err)
		}
	})

	t.Run("200 with unparseable response", func(t *testing.T) {
		srv, _ := geminiCoverServer(t, func(call int, r *http.Request) (int, string) {
			return http.StatusOK, `not-json`
		})
		p, _ := newCoveredGemini(t, srv, 3)

		_, err := p.ExtractMarketplaceInfo(context.Background(), "فروش آیفون")
		if err == nil || !strings.Contains(err.Error(), "unmarshal gemini response") {
			t.Fatalf("err = %v, want unmarshal error", err)
		}
	})

	t.Run("200 with invalid extraction json", func(t *testing.T) {
		srv, _ := geminiCoverServer(t, func(call int, r *http.Request) (int, string) {
			// Valid outer JSON, but the model "text" is not extraction JSON.
			return http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"{invalid json"}]}}]}`
		})
		p, _ := newCoveredGemini(t, srv, 3)

		_, err := p.ExtractMarketplaceInfo(context.Background(), "فروش آیفون")
		if err == nil || !strings.Contains(err.Error(), "unmarshal gemini json output") {
			t.Fatalf("err = %v, want unmarshal json output error", err)
		}
	})
}

func TestGeminiCallWithRetryErrorPaths(t *testing.T) {
	t.Run("request creation fails for invalid model name", func(t *testing.T) {
		// A control character in the model name makes the hardcoded endpoint
		// unparseable, so http.NewRequestWithContext fails before any I/O.
		p := NewGeminiProvider("test-key", "bad\nmodel", 5, 1)
		_, _, err := p.callWithRetry(context.Background(), geminiRequest{})
		if err == nil || !strings.Contains(err.Error(), "invalid control character in URL") {
			t.Fatalf("err = %v, want URL control character error", err)
		}
	})

	t.Run("context done wins during http-error backoff", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		p := NewGeminiProvider("test-key", "redirected", 5, 2)
		p.httpClient.Transport = coverTransport{Target: "http://127.0.0.1:1", Calls: new(int32), forwarder: &http.Client{}}
		start := time.Now()
		_, _, err := p.callWithRetry(ctx, geminiRequest{})
		if err != context.Canceled {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if time.Since(start) > time.Second {
			t.Errorf("backoff delay was not skipped after cancellation: %v", time.Since(start))
		}
	})

	t.Run("body read error is retried without backoff", func(t *testing.T) {
		// Advertise a long Content-Length then close the connection, forcing an
		// unexpected EOF while the client reads the body.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("server does not support hijacking")
			}
			conn, buf, err := hijacker.Hijack()
			if err != nil {
				t.Fatalf("hijack: %v", err)
			}
			respBytes := []byte("HTTP/1.1 200 OK\r\nContent-Length: 1000\r\n\r\n{\"par")
			if _, err := buf.Write(respBytes); err != nil {
				t.Fatalf("write: %v", err)
			}
			if err := buf.Flush(); err != nil {
				t.Fatalf("flush: %v", err)
			}
			_ = conn.Close()
		}))
		t.Cleanup(srv.Close)

		p := NewGeminiProvider("test-key", "redirected", 5, 3)
		var calls int32
		p.httpClient.Transport = coverTransport{Target: srv.URL, Calls: &calls, forwarder: &http.Client{}}

		_, _, err := p.callWithRetry(context.Background(), geminiRequest{})
		if err == nil || !strings.Contains(err.Error(), "exceeded max retries") {
			t.Fatalf("err = %v, want retry exhaustion from read error", err)
		}
		if got := atomic.LoadInt32(&calls); got != 3 {
			t.Errorf("server calls = %d, want 3", got)
		}
	})
}

func TestGeminiGenerateConversationQuestion(t *testing.T) {
	t.Run("empty api key uses fallback without network", func(t *testing.T) {
		p := NewGeminiProvider("", "redirected", 1, 1)
		q, err := p.GenerateConversationQuestion(context.Background(), "آیفون 15", domain.FieldPrice, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := fallbackQuestion(domain.FieldPrice, "آیفون 15")
		if q != want {
			t.Errorf("question = %q, want fallback %q", q, want)
		}
	})

	t.Run("server error falls back", func(t *testing.T) {
		srv, _ := geminiCoverServer(t, func(call int, r *http.Request) (int, string) {
			return http.StatusInternalServerError, `{"error":{"code":500,"message":"boom","status":"INTERNAL"}}`
		})
		p, _ := newCoveredGemini(t, srv, 1)

		q, err := p.GenerateConversationQuestion(context.Background(), "آیفون 15", domain.FieldPrice,
			[]domain.ConversationMessage{{Sender: domain.SenderSeller, Text: "سلام"}})
		if err != nil {
			t.Fatalf("expected fallback, got error: %v", err)
		}
		if q != fallbackQuestion(domain.FieldPrice, "آیفون 15") {
			t.Errorf("question = %q, want fallback", q)
		}
	})

	t.Run("accepted question from model", func(t *testing.T) {
		srv, _ := geminiCoverServer(t, func(call int, r *http.Request) (int, string) {
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), "Target field: price") || !strings.Contains(string(raw), "seller: سلام") {
				t.Errorf("request body missing target/history: %s", string(raw))
			}
			return http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"  قیمت چقدر است؟  "}]}}]}`
		})
		p, _ := newCoveredGemini(t, srv, 1)

		q, err := p.GenerateConversationQuestion(context.Background(), "آیفون 15", domain.FieldPrice,
			[]domain.ConversationMessage{{Sender: domain.SenderSeller, Text: "سلام"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if q != "قیمت چقدر است؟" {
			t.Errorf("question = %q, want trimmed model answer", q)
		}
	})

	for _, tc := range []struct {
		name string
		text string
	}{
		{"too long", strings.Repeat("ا", 301)},
		{"contains newline", "خط اول\nخط دوم"},
		{"contains http", "مشخصات را ببینید http://example.com"},
	} {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			srv, _ := geminiCoverServer(t, func(call int, r *http.Request) (int, string) {
				// Literal control characters are not valid JSON; escape them so the
				// provider parses a text containing a real newline.
				return http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":` + `"` + strings.ReplaceAll(tc.text, "\n", "\\n") + `"` + `}]}}]}`
			})
			p, _ := newCoveredGemini(t, srv, 1)

			q, err := p.GenerateConversationQuestion(context.Background(), "آیفون 15", domain.FieldCondition, nil)
			if err != nil {
				t.Fatalf("expected fallback, got error: %v", err)
			}
			if q != fallbackQuestion(domain.FieldCondition, "آیفون 15") {
				t.Errorf("question = %q, want fallback", q)
			}
		})
	}
}

func TestGeminiNewProviderDefaults(t *testing.T) {
	p := NewGeminiProvider("", "", 0, 0)
	if p.model != "gemini-2.5-flash" {
		t.Errorf("model = %q, want default gemini-2.5-flash", p.model)
	}
	if p.timeout != 15*time.Second || p.httpClient.Timeout != 15*time.Second {
		t.Errorf("timeout = %v / %v, want 15s", p.timeout, p.httpClient.Timeout)
	}
	if p.maxRetries != 3 {
		t.Errorf("maxRetries = %d, want 3", p.maxRetries)
	}
	if p.Name() != "gemini (gemini-2.5-flash)" {
		t.Errorf("Name() = %q", p.Name())
	}

	p2 := NewGeminiProvider("k", "custom-model", 2, 1)
	if p2.model != "custom-model" || p2.timeout != 2*time.Second || p2.maxRetries != 1 {
		t.Errorf("explicit values overridden: %+v", p2)
	}
	if p2.Name() != "gemini (custom-model)" {
		t.Errorf("Name() = %q", p2.Name())
	}
}

func TestGeminiValidateConsentResponse(t *testing.T) {
	p := NewGeminiProvider("key", "redirected", 1, 1)

	consent, err := p.ValidateConsentResponse(context.Background(), "بله حتماً")
	if err != nil || !consent {
		t.Errorf("affirmative: consent=%v err=%v, want true,nil", consent, err)
	}
	consent, err = p.ValidateConsentResponse(context.Background(), "نه")
	if err != nil || consent {
		t.Errorf("refusal: consent=%v err=%v, want false,nil", consent, err)
	}
	consent, err = p.ValidateConsentResponse(context.Background(), "متن مبهم")
	if err != nil || consent {
		t.Errorf("ambiguous: consent=%v err=%v, want false,nil", consent, err)
	}
}

func TestConsentDecision(t *testing.T) {
	if decision, known := ConsentDecision("اوکی"); !decision || !known {
		t.Errorf("ConsentDecision(اوکی) = %v,%v want true,true", decision, known)
	}
	if decision, known := ConsentDecision("نه"); decision || !known {
		t.Errorf("ConsentDecision(نه) = %v,%v want false,true", decision, known)
	}
	if decision, known := ConsentDecision("هر چی بگم"); decision || known {
		t.Errorf("ConsentDecision(ambiguous) = %v,%v want false,false", decision, known)
	}
}

func TestDeterministicConsentPositiveMatches(t *testing.T) {
	positives := []string{"بله", "اوکی", "موافقم", "بپرس", "بپرسید", "در خدمتم", "بله بپرسید", "بله، بپرسید", "بله بفرمایید", "بله حتما", "بله حتماً", "yes", "yes please", "yes, please", "yes go ahead", "ok", "okay"}
	for _, text := range positives {
		if decision, known := deterministicConsent(text); !decision || !known {
			t.Errorf("deterministicConsent(%q) = %v,%v want true,true", text, decision, known)
		}
	}
	// Trimmed punctuation around a positive token still matches.
	if decision, known := deterministicConsent("بله !"); !decision || !known {
		t.Errorf("deterministicConsent(%q) = %v,%v want true,true", "بله !", decision, known)
	}
}

func TestDeterministicConsentRefusalTokens(t *testing.T) {
	refusals := []string{"خیر", "نه", "تمایلی ندارم", "پیام نده", "مزاحم نشو", "موافق نیستم", "no", "stop"}
	for _, text := range refusals {
		if decision, known := deterministicConsent(text); decision || !known {
			t.Errorf("deterministicConsent(%q) = %v,%v want false,true", text, decision, known)
		}
	}
	// Refusal wins even when embedded alongside an affirmative.
	if decision, known := deterministicConsent("بله ولی نه"); decision || !known {
		t.Errorf("mixed = %v,%v want false,true", decision, known)
	}
}

func TestGeminiFallbackQuestion(t *testing.T) {
	cases := map[domain.MissingField]string{
		domain.FieldPrice:        "سلام! من دستیار خودکار پرسو هستم. قیمت پیشنهادی شما برای p چقدر است؟",
		domain.FieldBranded:      "دستگاه p اصلی و اورجینال شرکتی است یا های‌کپی؟",
		domain.FieldManufacturer: "سازنده و پارت نامبر دقیق p را بفرمایید.",
		domain.FieldCity:         "دستگاه در کدام شهر قرار دارد و امکان تحویل یا ارسال حضوری کجاست؟",
		domain.FieldTehranArea:   "برای تحویل در تهران، در کدام محدوده یا بازار (مانند علاءالدین یا چارسو) امکان مراجعه وجود دارد؟",
		domain.FieldExpiration:   "آیا گارانتی یا مهلت تست دارد و تا چه تاریخی معتبر است؟",
		domain.FieldCondition:    "برای تکمیل اطلاعات p، جزئیات بیشتر را بفرمایید.",
	}
	for field, want := range cases {
		if got := fallbackQuestion(field, "p"); got != want {
			t.Errorf("fallbackQuestion(%s) = %q, want %q", field, got, want)
		}
	}
}

func TestMockProviderCoverage(t *testing.T) {
	m := NewMockProvider()
	if m.Name() != "mock-llm-provider" {
		t.Errorf("Name() = %q, want mock-llm-provider", m.Name())
	}

	m.SimulateError = true
	if _, err := m.ExtractMarketplaceInfo(context.Background(), "x"); err != context.DeadlineExceeded {
		t.Errorf("SimulateError: err = %v, want context.DeadlineExceeded", err)
	}

	custom := &LLMExtractionResponse{Classification: domain.ClassificationBuyer, Confidence: 0.5}
	m.SimulateError = false
	m.CustomResponse = custom
	got, err := m.ExtractMarketplaceInfo(context.Background(), "x")
	if err != nil || got != custom {
		t.Errorf("CustomResponse: got=%p err=%v, want same pointer %p", got, err, custom)
	}

	m.CustomResponse = nil
	q, err := m.GenerateConversationQuestion(context.Background(), "آیفون", domain.FieldPrice, nil)
	if err != nil || q != fallbackQuestion(domain.FieldPrice, "آیفون") {
		t.Errorf("GenerateConversationQuestion = %q,%v", q, err)
	}

	if consent, err := m.ValidateConsentResponse(context.Background(), "بله"); err != nil || !consent {
		t.Errorf("ValidateConsentResponse(بله) = %v,%v", consent, err)
	}
}
