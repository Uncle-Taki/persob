package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
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

	// LLM
	LLMEnabled     bool
	LLMProvider    string
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

		// LLM disabled by default for privacy & Telegram Terms compliance
		LLMEnabled:     getEnvAsBool("LLM_ENABLED", false),
		LLMProvider:    getEnv("LLM_PROVIDER", "gemini"),
		GeminiAPIKey:   getEnv("GEMINI_API_KEY", ""),
		GeminiModel:    getEnv("GEMINI_MODEL", "gemini-2.5-flash"),
		LLMTimeoutSecs: getEnvAsInt("LLM_TIMEOUT_SECONDS", 15),
		LLMMaxRetries:  getEnvAsInt("LLM_MAX_RETRIES", 3),

		WorkerConcurrency: getEnvAsInt("WORKER_CONCURRENCY", 4),
		BatchSize:         getEnvAsInt("BATCH_SIZE", 20),
		PollInterval:      time.Duration(getEnvAsInt("POLL_INTERVAL_MS", 1000)) * time.Millisecond,

		ConversationMaxQuestions: getEnvAsInt("CONVERSATION_MAX_QUESTIONS", 3),
		ConversationCooldown:     time.Duration(getEnvAsInt("CONVERSATION_COOLDOWN_HOURS", 24)) * time.Hour,
		ConversationAutoApprove:  getEnvAsBool("CONVERSATION_AUTO_APPROVE", false),
	}

	if cfg.DemoAuthEnabled && cfg.Environment != "development" && cfg.Environment != "test" {
		return nil, fmt.Errorf("DEMO_AUTH_ENABLED is permitted only in development or test")
	}
	if cfg.LLMEnabled && (cfg.LLMProvider != "gemini" || cfg.GeminiAPIKey == "") {
		return nil, fmt.Errorf("LLM_ENABLED requires the supported gemini provider and an API key")
	}
	if !cfg.TelegramMockMode && cfg.TelegramAppID != 0 && (cfg.TelegramSessionStorageKey == "" || cfg.TelegramSessionStorageKey == "perso-mtproto-session-encryption-key-32b!") {
		return nil, fmt.Errorf("real Telegram mode requires a non-default session encryption key")
	}
	return cfg, nil
}

func (c *Config) PostgresDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.PostgresUser, c.PostgresPassword, c.PostgresHost, c.PostgresPort, c.PostgresDB, c.PostgresSSLMode)
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
