package config

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	BootstrapUsername    string
	BootstrapPassword    string
	AuthCookieSecure     bool
	OutboundInterval     time.Duration
	QuietStart, QuietEnd int
	OutboundTimezone     string

	ConversationFields []domain.MissingField

	OutreachEnabled bool

	APIToken              string
	OperatorUserID        string
	GatewayAddr           string
	GatewayURL            string
	GatewayToken          string
	TelegramBackfillLimit int
	RetentionDays         int

	Environment     string
	Port            int
	Host            string
	LogLevel        string
	DemoAuthEnabled bool

	// PostgreSQL
	PostgresHost     string
	PostgresPort     int
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string
	PostgresSSLMode  string

	// MongoDB
	MongoURI string
	MongoDB  string

	// Redis
	RedisAddr          string
	RedisPassword      string
	RedisDB            int
	RedisStreamKey     string
	RedisConsumerGroup string
	RedisConsumerName  string

	// Telegram
	TelegramAppID             int
	TelegramAppHash           string
	TelegramPhoneNumber       string
	TelegramSessionDir        string
	TelegramSessionStorageKey string
	TelegramMockMode          bool
	TelegramMockHealthcare    bool

	// LLM
	LLMEnabled     bool
	LLMProvider    string
	LLMBaseURL     string
	LLMModel       string
	LLMAPIKey      string
	GeminiAPIKey   string
	GeminiModel    string
	LLMTimeoutSecs int
	LLMMaxRetries  int

	// Worker
	WorkerConcurrency int
	BatchSize         int
	PollInterval      time.Duration

	// Conversation
	ConversationMaxQuestions int
	ConversationCooldown     time.Duration
	ConversationAutoApprove  bool
}

func Load() (*Config, error) {
	cfg := &Config{
		BootstrapUsername: getEnv("BOOTSTRAP_ADMIN_USERNAME", ""),
		BootstrapPassword: getEnv("BOOTSTRAP_ADMIN_PASSWORD", ""),
		AuthCookieSecure:  getEnvAsBool("AUTH_COOKIE_SECURE", getEnv("ENVIRONMENT", "development") == "production"),
		OutboundInterval:  time.Duration(getEnvAsInt("OUTBOUND_INTERVAL_SECONDS", 5)) * time.Second, OutboundTimezone: getEnv("OUTBOUND_TIMEZONE", "Asia/Tehran"),
		OutreachEnabled: getEnvAsBool("OUTREACH_ENABLED", false),
		APIToken:        getEnv("API_TOKEN", ""), OperatorUserID: getEnv("OPERATOR_USER_ID", "11111111-1111-1111-1111-111111111111"),
		GatewayAddr: getEnv("TELEGRAM_GATEWAY_ADDR", "0.0.0.0:8090"), GatewayURL: getEnv("TELEGRAM_GATEWAY_URL", "http://127.0.0.1:8090"), GatewayToken: getEnv("TELEGRAM_GATEWAY_TOKEN", ""),
		TelegramBackfillLimit: getEnvAsInt("TELEGRAM_BACKFILL_LIMIT", 100), RetentionDays: getEnvAsInt("RETENTION_DAYS", 0),

		Environment:     getEnv("ENVIRONMENT", "development"),
		Port:            getEnvAsInt("PORT", 8080),
		Host:            getEnv("HOST", "0.0.0.0"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		DemoAuthEnabled: getEnvAsBool("DEMO_AUTH_ENABLED", false),

		PostgresHost:     getEnv("POSTGRES_HOST", "localhost"),
		PostgresPort:     getEnvAsInt("POSTGRES_PORT", 5432),
		PostgresUser:     getEnv("POSTGRES_USER", "perso"),
		PostgresPassword: getEnv("POSTGRES_PASSWORD", "perso_secret_change_me"),
		PostgresDB:       getEnv("POSTGRES_DB", "perso_db"),
		PostgresSSLMode:  getEnv("POSTGRES_SSLMODE", "disable"),

		MongoURI: getEnv("MONGODB_URI", "mongodb://perso_user:perso_mongo_pass@localhost:27017/perso_raw?authSource=admin"),
		MongoDB:  getEnv("MONGODB_DATABASE", "perso_raw"),

		RedisAddr:          getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:      getEnv("REDIS_PASSWORD", ""),
		RedisDB:            getEnvAsInt("REDIS_DB", 0),
		RedisStreamKey:     getEnv("REDIS_STREAM_KEY", "perso:telegram:messages"),
		RedisConsumerGroup: getEnv("REDIS_CONSUMER_GROUP", "perso:message_workers"),
		RedisConsumerName:  getEnv("REDIS_CONSUMER_NAME", "worker-1"),

		TelegramAppID:             getEnvAsInt("TELEGRAM_APP_ID", 0),
		TelegramAppHash:           getEnv("TELEGRAM_APP_HASH", ""),
		TelegramPhoneNumber:       getEnv("TELEGRAM_PHONE_NUMBER", ""),
		TelegramSessionDir:        getEnv("TELEGRAM_SESSION_DIR", "./sessions"),
		TelegramSessionStorageKey: getEnv("TELEGRAM_SESSION_STORAGE_KEY", "perso-mtproto-session-encryption-key-32b!"),
		TelegramMockMode:          getEnvAsBool("TELEGRAM_MOCK_MODE", false),
		TelegramMockHealthcare:    getEnvAsBool("TELEGRAM_MOCK_HEALTHCARE", false),

		// LLM disabled by default for privacy & Telegram Terms compliance
		LLMEnabled:     getEnvAsBool("LLM_ENABLED", false),
		LLMProvider:    getEnv("LLM_PROVIDER", "gemini"),
		LLMBaseURL:     getEnv("LLM_BASE_URL", "http://llm-mock:8091/v1"),
		LLMModel:       getEnv("LLM_MODEL", "perso-healthcare-mock"),
		LLMAPIKey:      getEnv("LLM_API_KEY", ""),
		GeminiAPIKey:   getEnv("GEMINI_API_KEY", ""),
		GeminiModel:    getEnv("GEMINI_MODEL", "gemini-2.5-flash"),
		LLMTimeoutSecs: getEnvAsInt("LLM_TIMEOUT_SECONDS", 15),
		LLMMaxRetries:  getEnvAsInt("LLM_MAX_RETRIES", 3),

		WorkerConcurrency: getEnvAsInt("WORKER_CONCURRENCY", 4),
		BatchSize:         getEnvAsInt("BATCH_SIZE", 20),
		PollInterval:      time.Duration(getEnvAsInt("POLL_INTERVAL_MS", 1000)) * time.Millisecond,

		ConversationMaxQuestions: getEnvAsInt("CONVERSATION_MAX_QUESTIONS", 7),
		ConversationCooldown:     time.Duration(getEnvAsInt("CONVERSATION_COOLDOWN_HOURS", 24)) * time.Hour,
		ConversationAutoApprove:  getEnvAsBool("CONVERSATION_AUTO_APPROVE", false),
	}

	if cfg.TelegramMockHealthcare && (!cfg.TelegramMockMode || (cfg.Environment != "development" && cfg.Environment != "test")) {
		return nil, fmt.Errorf("TELEGRAM_MOCK_HEALTHCARE requires development/test and TELEGRAM_MOCK_MODE")
	}
	if cfg.Environment == "production" && !cfg.AuthCookieSecure {
		return nil, fmt.Errorf("production requires AUTH_COOKIE_SECURE=true")
	}
	if cfg.DemoAuthEnabled && cfg.Environment != "development" && cfg.Environment != "test" {
		return nil, fmt.Errorf("DEMO_AUTH_ENABLED is permitted only in development or test")
	}
	if cfg.LLMEnabled {
		switch cfg.LLMProvider {
		case "gemini":
			if cfg.GeminiAPIKey == "" {
				return nil, fmt.Errorf("LLM_ENABLED with gemini requires GEMINI_API_KEY")
			}
		case "openai_compatible":
			if cfg.LLMBaseURL == "" || cfg.LLMModel == "" {
				return nil, fmt.Errorf("LLM_ENABLED with openai_compatible requires LLM_BASE_URL and LLM_MODEL")
			}
		default:
			return nil, fmt.Errorf("LLM_ENABLED requires provider gemini or openai_compatible")
		}
	}
	if !cfg.TelegramMockMode && cfg.TelegramAppID != 0 && (cfg.TelegramSessionStorageKey == "" || cfg.TelegramSessionStorageKey == "perso-mtproto-session-encryption-key-32b!") {
		return nil, fmt.Errorf("real Telegram mode requires a non-default session encryption key")
	}
	if cfg.OutboundInterval < time.Second {
		return nil, fmt.Errorf("OUTBOUND_INTERVAL_SECONDS must be positive")
	}
	if _, err := time.LoadLocation(cfg.OutboundTimezone); err != nil {
		return nil, fmt.Errorf("OUTBOUND_TIMEZONE is invalid")
	}
	if hours := getEnv("OUTREACH_QUIET_HOURS", ""); hours != "" {
		parts := strings.Split(hours, "-")
		if len(parts) != 2 {
			return nil, fmt.Errorf("OUTREACH_QUIET_HOURS must use start-end hours")
		}
		var err error
		cfg.QuietStart, err = strconv.Atoi(parts[0])
		if err != nil || cfg.QuietStart < 0 || cfg.QuietStart > 23 {
			return nil, fmt.Errorf("invalid quiet start hour")
		}
		cfg.QuietEnd, err = strconv.Atoi(parts[1])
		if err != nil || cfg.QuietEnd < 0 || cfg.QuietEnd > 23 {
			return nil, fmt.Errorf("invalid quiet end hour")
		}
	}
	for _, field := range strings.Split(getEnv("CONVERSATION_FIELDS", "price,condition,branded,manufacturer,warranty_until,seller_city,seller_tehran_area"), ",") {
		value := domain.MissingField(strings.TrimSpace(field))
		switch value {
		case domain.FieldPrice, domain.FieldCondition, domain.FieldBranded, domain.FieldManufacturer, domain.FieldWarranty, domain.FieldExpiration, domain.FieldCity, domain.FieldTehranArea:
			cfg.ConversationFields = append(cfg.ConversationFields, value)
		default:
			return nil, fmt.Errorf("unsupported CONVERSATION_FIELDS entry")
		}
	}
	if _, err := uuid.Parse(cfg.OperatorUserID); err != nil {
		return nil, fmt.Errorf("OPERATOR_USER_ID must be a UUID")
	}
	if cfg.APIToken != "" && len(cfg.APIToken) < 32 {
		return nil, fmt.Errorf("API_TOKEN must contain at least 32 characters")
	}
	if cfg.GatewayToken != "" && len(cfg.GatewayToken) < 32 {
		return nil, fmt.Errorf("TELEGRAM_GATEWAY_TOKEN must contain at least 32 characters")
	}
	if cfg.WorkerConcurrency < 1 || cfg.WorkerConcurrency > 32 {
		return nil, fmt.Errorf("WORKER_CONCURRENCY must be between 1 and 32")
	}
	if cfg.TelegramBackfillLimit < 0 || cfg.TelegramBackfillLimit > 1000 {
		return nil, fmt.Errorf("TELEGRAM_BACKFILL_LIMIT must be between 0 and 1000")
	}
	return cfg, nil
}

func (c *Config) PostgresDSN() string {
	u := url.URL{Scheme: "postgres", User: url.UserPassword(c.PostgresUser, c.PostgresPassword), Host: net.JoinHostPort(c.PostgresHost, strconv.Itoa(c.PostgresPort)), Path: c.PostgresDB}
	query := url.Values{}
	query.Set("sslmode", c.PostgresSSLMode)
	u.RawQuery = query.Encode()
	return u.String()
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	valStr := getEnv(key, "")
	if val, err := strconv.Atoi(valStr); err == nil {
		return val
	}
	return defaultVal
}

func getEnvAsBool(key string, defaultVal bool) bool {
	valStr := getEnv(key, "")
	if val, err := strconv.ParseBool(valStr); err == nil {
		return val
	}
	return defaultVal
}

// OwnerID is the single configured operator/workspace identity, never supplied by a request.
func (c *Config) OwnerID() *uuid.UUID {
	id, err := uuid.Parse(c.OperatorUserID)
	if err != nil {
		id = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	}
	return &id
}

func (c *Config) InQuietHours(now time.Time) bool {
	if c.QuietStart == c.QuietEnd {
		return false
	}
	location, err := time.LoadLocation(c.OutboundTimezone)
	if err != nil {
		return true
	}
	hour := now.In(location).Hour()
	if c.QuietStart < c.QuietEnd {
		return hour >= c.QuietStart && hour < c.QuietEnd
	}
	return hour >= c.QuietStart || hour < c.QuietEnd
}
