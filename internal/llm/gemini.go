package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/perso/perso-mind/internal/domain"
)

type GeminiProvider struct {
	apiKey     string
	model      string
	httpClient *http.Client
	maxRetries int
	timeout    time.Duration
}

func NewGeminiProvider(apiKey, model string, timeoutSecs, maxRetries int) *GeminiProvider {
	if model == "" {
		model = "gemini-2.5-flash"
	}
	if timeoutSecs <= 0 {
		timeoutSecs = 15
	}
	if maxRetries <= 0 {
		maxRetries = 3
	}

	return &GeminiProvider{
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{Timeout: time.Duration(timeoutSecs) * time.Second},
		maxRetries: maxRetries,
		timeout:    time.Duration(timeoutSecs) * time.Second,
	}
}

func (p *GeminiProvider) Name() string {
	return "gemini (" + p.model + ")"
}

type geminiRequest struct {
	Contents          []geminiContent          `json:"contents"`
	SystemInstruction *geminiSystemInstruction `json:"systemInstruction,omitempty"`
	GenerationConfig  geminiGenConfig          `json:"generationConfig"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiSystemInstruction struct {
	Parts []geminiPart `json:"parts"`
}

type geminiGenConfig struct {
	ResponseMimeType string  `json:"responseMimeType,omitempty"`
	Temperature      float64 `json:"temperature"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

func (p *GeminiProvider) ExtractMarketplaceInfo(ctx context.Context, messageText string) (*LLMExtractionResponse, error) {
	if p.apiKey == "" {
		return nil, errors.New("gemini api key is empty")
	}

	safeWrappedText := SanitizeAndWrapPrompt(messageText)

	reqBody := geminiRequest{
		SystemInstruction: &geminiSystemInstruction{
			Parts: []geminiPart{{Text: SystemExtractionPrompt}},
		},
		Contents: []geminiContent{
			{
				Role:  "user",
				Parts: []geminiPart{{Text: safeWrappedText}},
			},
		},
		GenerationConfig: geminiGenConfig{
			ResponseMimeType: "application/json",
			Temperature:      0.1,
		},
	}

	rawJSON, usage, err := p.callWithRetry(ctx, reqBody)
	if err != nil {
		return nil, fmt.Errorf("gemini api call failed: %w", err)
	}

	var extraction LLMExtractionResponse
	if err := json.Unmarshal([]byte(rawJSON), &extraction); err != nil {
		return nil, fmt.Errorf("unmarshal gemini json output: %w, raw: %s", err, rawJSON)
	}

	extraction.TokenUsage = usage

	if err := ValidateLLMOutput(&extraction); err != nil {
		return nil, fmt.Errorf("validate llm output: %w", err)
	}

	slog.Info("gemini extraction completed successfully",
		"model", p.model,
		"prompt_tokens", usage.PromptTokens,
		"completion_tokens", usage.CompletionTokens,
		"classification", extraction.Classification,
	)

	return &extraction, nil
}

func (p *GeminiProvider) GenerateConversationQuestion(
	ctx context.Context,
	productName string,
	targetField domain.MissingField,
	history []domain.ConversationMessage,
) (string, error) {
	if p.apiKey == "" {
		return fallbackQuestion(targetField, productName), nil
	}

	var histSummary strings.Builder
	for _, m := range history {
		histSummary.WriteString(fmt.Sprintf("%s: %s\n", m.Sender, m.Text))
	}

	prompt := fmt.Sprintf(`You are Perso's automated Persian marketplace assistant.
The seller is selling "%s".
Target missing field to inquire: "%s".
Previous conversation history:
%s

Generate ONE single polite, concise, natural Persian question asking only for this missing field.
Do not ask for an exact address. Never negotiate or make purchase offers.
Introduce as Perso automated assistant if this is the first message.
Output ONLY the Persian question text, without quotation marks or explanations.`,
		productName, targetField, histSummary.String())

	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Role:  "user",
				Parts: []geminiPart{{Text: prompt}},
			},
		},
		GenerationConfig: geminiGenConfig{
			Temperature: 0.2,
		},
	}

	rawText, _, err := p.callWithRetry(ctx, reqBody)
	if err != nil {
		slog.Warn("gemini question generation failed, using fallback", "error", err)
		return fallbackQuestion(targetField, productName), nil
	}

	return strings.TrimSpace(rawText), nil
}

func (p *GeminiProvider) ValidateConsentResponse(ctx context.Context, sellerReply string) (bool, error) {
	lower := strings.ToLower(strings.TrimSpace(sellerReply))
	// Fast deterministic path
	if strings.Contains(lower, "بله") || strings.Contains(lower, "اوکی") || strings.Contains(lower, "موافقم") || strings.Contains(lower, "بپرس") || strings.Contains(lower, "در خدمتم") {
		return true, nil
	}
	if strings.Contains(lower, "خیر") || strings.Contains(lower, "نه") || strings.Contains(lower, "تمایلی ندارم") || strings.Contains(lower, "پیام نده") || strings.Contains(lower, "مزاحم نشو") {
		return false, nil
	}

	if p.apiKey == "" {
		return false, nil
	}

	prompt := fmt.Sprintf(`A marketplace seller was asked: "Can I ask a few short questions about your advertisement?"
Their reply: "%s"
Did the seller express consent/willingness to answer?
Respond with JSON: {"consent": true} or {"consent": false}`, sellerReply)

	reqBody := geminiRequest{
		Contents: []geminiContent{
			{Role: "user", Parts: []geminiPart{{Text: prompt}}},
		},
		GenerationConfig: geminiGenConfig{
			ResponseMimeType: "application/json",
			Temperature:      0.0,
		},
	}

	raw, _, err := p.callWithRetry(ctx, reqBody)
	if err != nil {
		return false, err
	}

	var res struct {
		Consent bool `json:"consent"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return false, err
	}
	return res.Consent, nil
}

func (p *GeminiProvider) callWithRetry(ctx context.Context, req geminiRequest) (string, TokenUsage, error) {
	endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", p.model, p.apiKey)

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("marshal gemini request: %w", err)
	}

	var lastErr error
	backoff := 500 * time.Millisecond

	for attempt := 1; attempt <= p.maxRetries; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBytes))
		if err != nil {
			return "", TokenUsage{}, err
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := p.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			slog.Warn("gemini http error, retrying", "attempt", attempt, "error", err)
			time.Sleep(backoff)
			backoff *= 2
			continue
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("gemini api returned status %d: %s", resp.StatusCode, string(bodyBytes))
			if resp.StatusCode == 429 || resp.StatusCode >= 500 {
				slog.Warn("gemini rate limit or server error, retrying", "status", resp.StatusCode, "attempt", attempt)
				time.Sleep(backoff)
				backoff *= 2
				continue
			}
			return "", TokenUsage{}, lastErr
		}

		var geminiResp geminiResponse
		if err := json.Unmarshal(bodyBytes, &geminiResp); err != nil {
			return "", TokenUsage{}, fmt.Errorf("unmarshal gemini response: %w", err)
		}

		if geminiResp.Error != nil {
			return "", TokenUsage{}, fmt.Errorf("gemini error (%d): %s", geminiResp.Error.Code, geminiResp.Error.Message)
		}

		if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
			return "", TokenUsage{}, errors.New("gemini returned empty candidate content")
		}

		text := geminiResp.Candidates[0].Content.Parts[0].Text
		usage := TokenUsage{
			PromptTokens:     geminiResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      geminiResp.UsageMetadata.TotalTokenCount,
		}

		return text, usage, nil
	}

	return "", TokenUsage{}, fmt.Errorf("gemini request exceeded max retries: %w", lastErr)
}

func fallbackQuestion(field domain.MissingField, productName string) string {
	switch field {
	case domain.FieldPrice:
		return fmt.Sprintf("سلام! من دستیار خودکار پرسو هستم. قیمت پیشنهادی شما برای %s چقدر است؟", productName)
	case domain.FieldBranded:
		return fmt.Sprintf("دستگاه %s اصلی و اورجینال شرکتی است یا های‌کپی؟", productName)
	case domain.FieldManufacturer:
		return fmt.Sprintf("سازنده و پارت نامبر دقیق %s را بفرمایید.", productName)
	case domain.FieldCity:
		return "دستگاه در کدام شهر قرار دارد و امکان تحویل یا ارسال حضوری کجاست؟"
	case domain.FieldTehranArea:
		return "برای تحویل در تهران، در کدام محدوده یا بازار (مانند علاءالدین یا چارسو) امکان مراجعه وجود دارد؟"
	case domain.FieldExpiration:
		return "آیا گارانتی یا مهلت تست دارد و تا چه تاریخی معتبر است؟"
	default:
		return fmt.Sprintf("برای تکمیل اطلاعات %s، جزئیات بیشتر را بفرمایید.", productName)
	}
}
