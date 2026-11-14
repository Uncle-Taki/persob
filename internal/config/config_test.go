package config

import "testing"

func TestUnsafeModesFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
	}{
		{"demo authentication in production", map[string]string{"ENVIRONMENT": "production", "DEMO_AUTH_ENABLED": "true"}},
		{"LLM without credentials", map[string]string{"LLM_ENABLED": "true", "GEMINI_API_KEY": ""}},
		{"unsupported LLM provider", map[string]string{"LLM_ENABLED": "true", "LLM_PROVIDER": "unsupported"}},
		{"real Telegram with default session key", map[string]string{"TELEGRAM_APP_ID": "1", "TELEGRAM_MOCK_MODE": "false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{"DEMO_AUTH_ENABLED", "LLM_ENABLED", "GEMINI_API_KEY", "TELEGRAM_APP_ID", "TELEGRAM_MOCK_MODE", "TELEGRAM_SESSION_STORAGE_KEY"} {
				t.Setenv(key, "")
			}
			for key, value := range tc.values {
				t.Setenv(key, value)
			}
			if _, err := Load(); err == nil {
				t.Fatal("unsafe mode accepted")
			}
		})
	}
}
