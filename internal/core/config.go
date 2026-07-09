package core

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address           string
	DatabaseURL       string
	JWTSecret         string
	JWTExpiration     time.Duration
	AllowedOrigins    []string
	CookieSecure      bool
	OfflineThreshold  time.Duration
	WarningThreshold  float64
	CriticalThreshold float64
	BootstrapServerID string
	BootstrapAPIKey   string
	BootstrapName     string
	BootstrapEmail    string
	BootstrapPassword string
	ShutdownTimeout   time.Duration
}

func LoadConfig() (Config, error) {
	cfg := Config{
		Address:           coreEnvOrDefault("TARISYA_CORE_ADDRESS", ":8081"),
		DatabaseURL:       strings.TrimSpace(os.Getenv("TARISYA_DATABASE_URL")),
		JWTSecret:         strings.TrimSpace(os.Getenv("TARISYA_JWT_SECRET")),
		JWTExpiration:     24 * time.Hour,
		AllowedOrigins:    splitCSV(coreEnvOrDefault("TARISYA_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:5173")),
		CookieSecure:      false,
		OfflineThreshold:  90 * time.Second,
		WarningThreshold:  80,
		CriticalThreshold: 90,
		BootstrapServerID: strings.TrimSpace(os.Getenv("TARISYA_BOOTSTRAP_SERVER_ID")),
		BootstrapAPIKey:   strings.TrimSpace(os.Getenv("TARISYA_BOOTSTRAP_API_KEY")),
		BootstrapName:     coreEnvOrDefault("TARISYA_BOOTSTRAP_USER_NAME", "Development User"),
		BootstrapEmail:    strings.ToLower(strings.TrimSpace(os.Getenv("TARISYA_BOOTSTRAP_USER_EMAIL"))),
		BootstrapPassword: os.Getenv("TARISYA_BOOTSTRAP_USER_PASSWORD"),
		ShutdownTimeout:   10 * time.Second,
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("TARISYA_DATABASE_URL is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return Config{}, errors.New("TARISYA_JWT_SECRET must contain at least 32 characters")
	}
	if value := strings.TrimSpace(os.Getenv("TARISYA_COOKIE_SECURE")); value != "" {
		secure, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, errors.New("TARISYA_COOKIE_SECURE must be true or false")
		}
		cfg.CookieSecure = secure
	}
	if value := strings.TrimSpace(os.Getenv("TARISYA_OFFLINE_THRESHOLD")); value != "" {
		threshold, err := time.ParseDuration(value)
		if err != nil || threshold < time.Second {
			return Config{}, errors.New("TARISYA_OFFLINE_THRESHOLD must be a duration of at least 1s")
		}
		cfg.OfflineThreshold = threshold
	}
	if value := strings.TrimSpace(os.Getenv("TARISYA_WARNING_THRESHOLD")); value != "" {
		threshold, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return Config{}, errors.New("TARISYA_WARNING_THRESHOLD must be a number")
		}
		cfg.WarningThreshold = threshold
	}
	if value := strings.TrimSpace(os.Getenv("TARISYA_CRITICAL_THRESHOLD")); value != "" {
		threshold, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return Config{}, errors.New("TARISYA_CRITICAL_THRESHOLD must be a number")
		}
		cfg.CriticalThreshold = threshold
	}
	if cfg.WarningThreshold < 0 || cfg.CriticalThreshold > 100 ||
		cfg.WarningThreshold >= cfg.CriticalThreshold {
		return Config{}, errors.New("health thresholds must satisfy 0 <= warning < critical <= 100")
	}
	if (cfg.BootstrapServerID == "") != (cfg.BootstrapAPIKey == "") {
		return Config{}, errors.New("TARISYA_BOOTSTRAP_SERVER_ID and TARISYA_BOOTSTRAP_API_KEY must be set together")
	}
	bootstrapUserFields := cfg.BootstrapEmail != "" || cfg.BootstrapPassword != ""
	if bootstrapUserFields && (cfg.BootstrapEmail == "" || len(cfg.BootstrapPassword) < 8) {
		return Config{}, errors.New("bootstrap user requires an email and password of at least 8 characters")
	}
	if cfg.BootstrapServerID != "" && cfg.BootstrapEmail == "" {
		return Config{}, errors.New("bootstrap server requires TARISYA_BOOTSTRAP_USER_EMAIL")
	}
	return cfg, nil
}

func coreEnvOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}
