package config

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
)

// configEnvKeys is every key Load() consults; clearing them isolates tests
// from the host environment (getEnv treats empty as unset).
var configEnvKeys = []string{
	"OUTBOUND_INTERVAL_SECONDS", "OUTBOUND_TIMEZONE", "OUTREACH_ENABLED",
	"API_TOKEN", "OPERATOR_USER_ID", "BOOTSTRAP_ADMIN_USERNAME", "BOOTSTRAP_ADMIN_PASSWORD", "AUTH_COOKIE_SECURE",
	"TELEGRAM_GATEWAY_ADDR", "TELEGRAM_GATEWAY_URL", "TELEGRAM_GATEWAY_TOKEN",
	"TELEGRAM_BACKFILL_LIMIT", "RETENTION_DAYS",
	"ENVIRONMENT", "PORT", "HOST", "LOG_LEVEL", "DEMO_AUTH_ENABLED",
	"POSTGRES_HOST", "POSTGRES_PORT", "POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB", "POSTGRES_SSLMODE",
	"MONGODB_URI", "MONGODB_DATABASE",
	"REDIS_ADDR", "REDIS_PASSWORD", "REDIS_DB", "REDIS_STREAM_KEY", "REDIS_CONSUMER_GROUP", "REDIS_CONSUMER_NAME",
	"TELEGRAM_APP_ID", "TELEGRAM_APP_HASH", "TELEGRAM_PHONE_NUMBER", "TELEGRAM_SESSION_DIR",
	"TELEGRAM_SESSION_STORAGE_KEY", "TELEGRAM_MOCK_MODE", "TELEGRAM_MOCK_HEALTHCARE",
	"LLM_ENABLED", "LLM_PROVIDER", "LLM_BASE_URL", "LLM_MODEL", "LLM_API_KEY", "GEMINI_API_KEY", "GEMINI_MODEL", "LLM_TIMEOUT_SECONDS", "LLM_MAX_RETRIES",
	"WORKER_CONCURRENCY", "BATCH_SIZE", "POLL_INTERVAL_MS",
	"CONVERSATION_MAX_QUESTIONS", "CONVERSATION_COOLDOWN_HOURS", "CONVERSATION_AUTO_APPROVE",
	"OUTREACH_QUIET_HOURS", "CONVERSATION_FIELDS",
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range configEnvKeys {
		t.Setenv(key, "")
	}
}

func TestLoadHappyPathDefaults(t *testing.T) {
	clearConfigEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.OutboundInterval != 5*time.Second {
		t.Errorf("OutboundInterval = %v, want 5s", cfg.OutboundInterval)
	}
	if cfg.OutboundTimezone != "Asia/Tehran" {
		t.Errorf("OutboundTimezone = %q, want Asia/Tehran", cfg.OutboundTimezone)
	}
	if cfg.OutreachEnabled || cfg.DemoAuthEnabled || cfg.LLMEnabled || cfg.TelegramMockMode || cfg.ConversationAutoApprove {
		t.Errorf("unsafe modes must default to false: %+v", cfg)
	}
	if cfg.OperatorUserID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("OperatorUserID = %q", cfg.OperatorUserID)
	}
	if cfg.GatewayAddr != "0.0.0.0:8090" || cfg.GatewayURL != "http://127.0.0.1:8090" || cfg.GatewayToken != "" {
		t.Errorf("gateway defaults = %q %q %q", cfg.GatewayAddr, cfg.GatewayURL, cfg.GatewayToken)
	}
	if cfg.TelegramBackfillLimit != 100 || cfg.RetentionDays != 0 {
		t.Errorf("backfill/retention = %d/%d", cfg.TelegramBackfillLimit, cfg.RetentionDays)
	}
	if cfg.Environment != "development" || cfg.Port != 8080 || cfg.Host != "0.0.0.0" || cfg.LogLevel != "info" {
		t.Errorf("server defaults = %q %d %q %q", cfg.Environment, cfg.Port, cfg.Host, cfg.LogLevel)
	}
	if cfg.PostgresHost != "localhost" || cfg.PostgresPort != 5432 || cfg.PostgresUser != "perso" ||
		cfg.PostgresPassword != "perso_secret_change_me" || cfg.PostgresDB != "perso_db" || cfg.PostgresSSLMode != "disable" {
		t.Errorf("postgres defaults = %+v", cfg)
	}
	if cfg.WorkerConcurrency != 4 || cfg.BatchSize != 20 || cfg.PollInterval != time.Second {
		t.Errorf("worker defaults = %+v", cfg)
	}
	if cfg.ConversationMaxQuestions != 7 || cfg.ConversationCooldown != 24*time.Hour {
		t.Errorf("conversation defaults = %+v", cfg)
	}
	if cfg.GeminiModel != "gemini-2.5-flash" || cfg.LLMTimeoutSecs != 15 || cfg.LLMMaxRetries != 3 {
		t.Errorf("llm defaults = %+v", cfg)
	}
	if cfg.LLMBaseURL != "http://llm-mock:8091/v1" || cfg.LLMModel != "perso-healthcare-mock" {
		t.Errorf("openai-compatible defaults = %q %q", cfg.LLMBaseURL, cfg.LLMModel)
	}
	if cfg.RedisAddr != "localhost:6379" || cfg.RedisDB != 0 {
		t.Errorf("redis defaults = %+v", cfg)
	}
	if cfg.TelegramAppID != 0 || cfg.TelegramSessionDir != "./sessions" {
		t.Errorf("telegram defaults = %+v", cfg)
	}

	wantFields := []domain.MissingField{
		domain.FieldPrice, domain.FieldCondition, domain.FieldBranded, domain.FieldManufacturer,
		domain.FieldWarranty, domain.FieldCity, domain.FieldTehranArea,
	}
	if len(cfg.ConversationFields) != len(wantFields) {
		t.Fatalf("ConversationFields = %v, want %v", cfg.ConversationFields, wantFields)
	}
	for i := range wantFields {
		if cfg.ConversationFields[i] != wantFields[i] {
			t.Errorf("ConversationFields[%d] = %v, want %v", i, cfg.ConversationFields[i], wantFields[i])
		}
	}
}

func TestLoadValidOverridesAccepted(t *testing.T) {
	for _, env := range []string{"test", "development"} {
		t.Run(env, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("ENVIRONMENT", env)
			t.Setenv("DEMO_AUTH_ENABLED", "true")
			t.Setenv("OUTREACH_QUIET_HOURS", "20-7")
			t.Setenv("CONVERSATION_FIELDS", " price , seller_city ")
			t.Setenv("LLM_ENABLED", "true")
			t.Setenv("LLM_PROVIDER", "gemini")
			t.Setenv("GEMINI_API_KEY", "test-key")
			t.Setenv("API_TOKEN", strings.Repeat("a", 32))
			t.Setenv("TELEGRAM_GATEWAY_TOKEN", strings.Repeat("b", 32))
			t.Setenv("WORKER_CONCURRENCY", "32")
			t.Setenv("TELEGRAM_BACKFILL_LIMIT", "1000")
			t.Setenv("TELEGRAM_APP_ID", "1")
			t.Setenv("TELEGRAM_MOCK_MODE", "true")

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() rejected valid overrides: %v", err)
			}
			if cfg.QuietStart != 20 || cfg.QuietEnd != 7 {
				t.Errorf("quiet hours = %d-%d, want 20-7", cfg.QuietStart, cfg.QuietEnd)
			}
			if len(cfg.ConversationFields) != 2 || cfg.ConversationFields[0] != domain.FieldPrice || cfg.ConversationFields[1] != domain.FieldCity {
				t.Errorf("ConversationFields = %v, want [price seller_city]", cfg.ConversationFields)
			}
			if !cfg.LLMEnabled || cfg.LLMProvider != "gemini" || cfg.GeminiAPIKey != "test-key" {
				t.Errorf("llm config = %+v", cfg)
			}
			if cfg.WorkerConcurrency != 32 || cfg.TelegramBackfillLimit != 1000 {
				t.Errorf("worker/backfill = %d/%d", cfg.WorkerConcurrency, cfg.TelegramBackfillLimit)
			}
			if len(cfg.APIToken) != 32 || len(cfg.GatewayToken) != 32 {
				t.Errorf("token lengths = %d/%d", len(cfg.APIToken), len(cfg.GatewayToken))
			}
		})
	}
}

func TestLoadOpenAICompatibleProvider(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("LLM_ENABLED", "true")
	t.Setenv("LLM_PROVIDER", "openai_compatible")
	t.Setenv("LLM_BASE_URL", "http://llm-mock:8091/v1")
	t.Setenv("LLM_MODEL", "perso-healthcare-mock")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() rejected openai-compatible provider: %v", err)
	}
	if cfg.LLMProvider != "openai_compatible" || cfg.LLMBaseURL == "" || cfg.LLMModel == "" {
		t.Fatalf("unexpected provider config: %+v", cfg)
	}
}

func TestLoadValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"insecure production cookie", map[string]string{"ENVIRONMENT": "production", "AUTH_COOKIE_SECURE": "false"}, "production requires AUTH_COOKIE_SECURE=true"},
		{"demo auth in production", map[string]string{"ENVIRONMENT": "production", "DEMO_AUTH_ENABLED": "true"}, "DEMO_AUTH_ENABLED is permitted only in development or test"},
		{"llm enabled without api key", map[string]string{"LLM_ENABLED": "true"}, "LLM_ENABLED with gemini requires GEMINI_API_KEY"},
		{"llm enabled unsupported provider", map[string]string{"LLM_ENABLED": "true", "LLM_PROVIDER": "openai", "GEMINI_API_KEY": "k"}, "LLM_ENABLED requires provider gemini or openai_compatible"},
		{"real telegram default session key", map[string]string{"TELEGRAM_APP_ID": "1"}, "real Telegram mode requires a non-default session encryption key"},
		{"real telegram empty session key", map[string]string{"TELEGRAM_APP_ID": "1", "TELEGRAM_SESSION_STORAGE_KEY": ""}, "real Telegram mode requires a non-default session encryption key"},
		{"outbound interval zero", map[string]string{"OUTBOUND_INTERVAL_SECONDS": "0"}, "OUTBOUND_INTERVAL_SECONDS must be positive"},
		{"invalid timezone", map[string]string{"OUTBOUND_TIMEZONE": "Mars/Olympus"}, "OUTBOUND_TIMEZONE is invalid"},
		{"quiet hours wrong part count", map[string]string{"OUTREACH_QUIET_HOURS": "1"}, "OUTREACH_QUIET_HOURS must use start-end hours"},
		{"quiet hours three parts", map[string]string{"OUTREACH_QUIET_HOURS": "1-2-3"}, "OUTREACH_QUIET_HOURS must use start-end hours"},
		{"quiet hours non-numeric start", map[string]string{"OUTREACH_QUIET_HOURS": "abc-def"}, "invalid quiet start hour"},
		// A negative start is unreachable with an ASCII minus because splitting
		// "-5-10" yields three parts; the hour-range check (24) and the parse
		// failure (abc) cover the full start-hour condition block.
		{"quiet hours start above 23", map[string]string{"OUTREACH_QUIET_HOURS": "24-10"}, "invalid quiet start hour"},
		{"quiet hours non-numeric end", map[string]string{"OUTREACH_QUIET_HOURS": "10-xyz"}, "invalid quiet end hour"},
		{"quiet hours end above 23", map[string]string{"OUTREACH_QUIET_HOURS": "10-24"}, "invalid quiet end hour"},
		{"unsupported conversation field", map[string]string{"CONVERSATION_FIELDS": "price,bogus"}, "unsupported CONVERSATION_FIELDS entry"},
		{"operator user id not a uuid", map[string]string{"OPERATOR_USER_ID": "not-a-uuid"}, "OPERATOR_USER_ID must be a UUID"},
		{"api token too short", map[string]string{"API_TOKEN": strings.Repeat("a", 31)}, "API_TOKEN must contain at least 32 characters"},
		{"gateway token too short", map[string]string{"TELEGRAM_GATEWAY_TOKEN": strings.Repeat("b", 31)}, "TELEGRAM_GATEWAY_TOKEN must contain at least 32 characters"},
		{"worker concurrency zero", map[string]string{"WORKER_CONCURRENCY": "0"}, "WORKER_CONCURRENCY must be between 1 and 32"},
		{"worker concurrency above 32", map[string]string{"WORKER_CONCURRENCY": "33"}, "WORKER_CONCURRENCY must be between 1 and 32"},
		{"backfill limit negative", map[string]string{"TELEGRAM_BACKFILL_LIMIT": "-1"}, "TELEGRAM_BACKFILL_LIMIT must be between 0 and 1000"},
		{"backfill limit above 1000", map[string]string{"TELEGRAM_BACKFILL_LIMIT": "1001"}, "TELEGRAM_BACKFILL_LIMIT must be between 0 and 1000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			_, err := Load()
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Load() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPostgresDSN(t *testing.T) {
	cfg := &Config{
		PostgresHost:     "db.internal",
		PostgresPort:     5433,
		PostgresUser:     "perso",
		PostgresPassword: "p@ss:word",
		PostgresDB:       "perso_db",
		PostgresSSLMode:  "require",
	}
	want := "postgres://perso:p%40ss%3Aword@db.internal:5433/perso_db?sslmode=require"
	if got := cfg.PostgresDSN(); got != want {
		t.Errorf("PostgresDSN() = %q, want %q", got, want)
	}
}

func TestOwnerID(t *testing.T) {
	valid := "22222222-2222-2222-2222-222222222222"
	cfg := &Config{OperatorUserID: valid}
	if got := cfg.OwnerID(); *got != uuid.MustParse(valid) {
		t.Errorf("OwnerID() = %s, want passthrough %s", got, valid)
	}

	cfg.OperatorUserID = "not-a-uuid"
	if got := cfg.OwnerID(); *got != uuid.MustParse("11111111-1111-1111-1111-111111111111") {
		t.Errorf("OwnerID() = %s, want fallback 11111111-1111-1111-1111-111111111111", got)
	}

	cfg.OperatorUserID = ""
	if got := cfg.OwnerID(); *got != uuid.MustParse("11111111-1111-1111-1111-111111111111") {
		t.Errorf("OwnerID() with empty id = %s, want fallback", got)
	}
}

func TestInQuietHours(t *testing.T) {
	at := func(hour int) time.Time {
		return time.Date(2026, 1, 15, hour, 30, 0, 0, time.UTC)
	}

	t.Run("equal start and end is never quiet", func(t *testing.T) {
		cfg := &Config{QuietStart: 5, QuietEnd: 5, OutboundTimezone: "UTC"}
		if cfg.InQuietHours(at(5)) {
			t.Error("expected false when QuietStart == QuietEnd")
		}
	})

	t.Run("invalid timezone fails open", func(t *testing.T) {
		cfg := &Config{QuietStart: 8, QuietEnd: 20, OutboundTimezone: "Mars/Olympus"}
		if !cfg.InQuietHours(at(12)) {
			t.Error("expected true for an unloadable timezone")
		}
	})

	t.Run("normal window", func(t *testing.T) {
		cfg := &Config{QuietStart: 8, QuietEnd: 20, OutboundTimezone: "UTC"}
		cases := []struct {
			hour int
			want bool
		}{
			{7, false}, {8, true}, {12, true}, {19, true}, {20, false}, {23, false},
		}
		for _, c := range cases {
			if got := cfg.InQuietHours(at(c.hour)); got != c.want {
				t.Errorf("InQuietHours(hour %d) = %v, want %v", c.hour, got, c.want)
			}
		}
	})

	t.Run("wrap-around window", func(t *testing.T) {
		cfg := &Config{QuietStart: 22, QuietEnd: 6, OutboundTimezone: "UTC"}
		cases := []struct {
			hour int
			want bool
		}{
			{0, true}, {5, true}, {6, false}, {12, false}, {21, false}, {22, true}, {23, true},
		}
		for _, c := range cases {
			if got := cfg.InQuietHours(at(c.hour)); got != c.want {
				t.Errorf("InQuietHours(hour %d) = %v, want %v", c.hour, got, c.want)
			}
		}
	})
}
