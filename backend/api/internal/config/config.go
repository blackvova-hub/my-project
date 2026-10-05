package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr          string
	TLSCertFile       string
	TLSKeyFile        string
	DatabaseURL       string
	CORSOrigin        string
	CookieSecure      bool
	HSTSMaxAge        int
	RedisAddr         string
	RedisPassword     string
	RedisDB           int
	SMTPHost          string
	SMTPPort          int
	SMTPUser          string
	SMTPPass          string
	SMTPFrom          string
	TelegramBotToken  string
	TelegramBotUser   string
	TelegramSecret    string
	TelegramPolling   bool
	TurnstileSecret   string
	TurnstileEnabled  bool
	OpenAIAPIKey      string
	OpenAIBaseURL     string
	OpenAIModel       string
	AIDailyTokens     int
	AppEnv            string
	FrontendURL       string
	ResetTokenSecret  string
	TrustedProxyCIDRs string
}

func Load() Config {
	appEnv := envStr("APP_ENV", "development")
	cookieSecure := envBool("COOKIE_SECURE", false)
	if strings.EqualFold(appEnv, "production") && !cookieSecure {
		panic("COOKIE_SECURE must be true in production")
	}
	return Config{
		HTTPAddr:          envStr("HTTP_ADDR", ":8080"),
		TLSCertFile:       envStr("TLS_CERT_FILE", ""),
		TLSKeyFile:        envStr("TLS_KEY_FILE", ""),
		DatabaseURL:       mustEnv("DATABASE_URL"),
		CORSOrigin:        envStr("CORS_ORIGIN", "http://localhost"),
		CookieSecure:      cookieSecure,
		HSTSMaxAge:        envInt("HSTS_MAX_AGE", 31536000),
		RedisAddr:         envStr("REDIS_ADDR", ""),
		RedisPassword:     envStr("REDIS_PASSWORD", ""),
		RedisDB:           envInt("REDIS_DB", 0),
		SMTPHost:          envStr("SMTP_HOST", "smtp.mailersend.net"),
		SMTPPort:          envInt("SMTP_PORT", 587),
		SMTPUser:          envStr("SMTP_USER", ""),
		SMTPPass:          envStr("SMTP_PASS", ""),
		SMTPFrom:          envStr("SMTP_FROM", ""),
		TelegramBotToken:  envStr("TELEGRAM_BOT_TOKEN", ""),
		TelegramBotUser:   envStr("TELEGRAM_BOT_USERNAME", ""),
		TelegramSecret:    envStr("TELEGRAM_WEBHOOK_SECRET", ""),
		TelegramPolling:   envBool("TELEGRAM_POLLING_ENABLED", false),
		TurnstileSecret:   envStr("TURNSTILE_SECRET", ""),
		TurnstileEnabled:  envBool("TURNSTILE_ENABLED", false),
		OpenAIAPIKey:      envStr("OPENAI_API_KEY", ""),
		OpenAIBaseURL:     envStr("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		OpenAIModel:       envStr("OPENAI_MODEL", "gpt-5.4-mini"),
		AIDailyTokens:     envInt("OPENAI_DAILY_TOKENS", 150000),
		AppEnv:            appEnv,
		FrontendURL:       envStr("FRONTEND_URL", ""),
		ResetTokenSecret:  envStr("RESET_TOKEN_SECRET", ""),
		TrustedProxyCIDRs: envStr("TRUSTED_PROXY_CIDRS", ""),
	}
}

func envStr(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}

func mustEnv(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		panic("missing env var: " + key)
	}
	return v
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}
