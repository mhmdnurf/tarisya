package core

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address                  string
	DatabaseURL              string
	PublicCoreURL            string
	JWTSecret                string
	JWTExpiration            time.Duration
	AllowedOrigins           []string
	CookieSecure             bool
	OfflineThreshold         time.Duration
	WarningThreshold         float64
	CriticalThreshold        float64
	BootstrapServerID        string
	BootstrapAPIKey          string
	BootstrapName            string
	BootstrapEmail           string
	BootstrapPassword        string
	ShutdownTimeout          time.Duration
	RetentionRaw             time.Duration
	Retention5m              time.Duration
	RetentionAggregated      time.Duration
	MaxDatabaseSize          int64
	DatabaseWarningThreshold float64
}

func LoadConfig() (Config, error) {
	cfg := Config{
		Address:                  coreEnvOrDefault("TARISYA_CORE_ADDRESS", ":8081"),
		DatabaseURL:              strings.TrimSpace(os.Getenv("TARISYA_DATABASE_URL")),
		PublicCoreURL:            coreEnvOrDefault("TARISYA_PUBLIC_CORE_URL", "http://localhost:8081"),
		JWTSecret:                strings.TrimSpace(os.Getenv("TARISYA_JWT_SECRET")),
		JWTExpiration:            24 * time.Hour,
		AllowedOrigins:           splitCSV(coreEnvOrDefault("TARISYA_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:5173")),
		CookieSecure:             false,
		OfflineThreshold:         90 * time.Second,
		WarningThreshold:         80,
		CriticalThreshold:        90,
		BootstrapServerID:        strings.TrimSpace(os.Getenv("TARISYA_BOOTSTRAP_SERVER_ID")),
		BootstrapAPIKey:          strings.TrimSpace(os.Getenv("TARISYA_BOOTSTRAP_API_KEY")),
		BootstrapName:            coreEnvOrDefault("TARISYA_BOOTSTRAP_USER_NAME", "Development User"),
		BootstrapEmail:           strings.ToLower(strings.TrimSpace(os.Getenv("TARISYA_BOOTSTRAP_USER_EMAIL"))),
		BootstrapPassword:        os.Getenv("TARISYA_BOOTSTRAP_USER_PASSWORD"),
		ShutdownTimeout:          10 * time.Second,
		RetentionRaw:             7 * 24 * time.Hour,
		Retention5m:              30 * 24 * time.Hour,
		RetentionAggregated:      90 * 24 * time.Hour,
		DatabaseWarningThreshold: 0.8,
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
	var err error
	if cfg.RetentionRaw, err = durationEnv("TARISYA_RETENTION_RAW", cfg.RetentionRaw); err != nil {
		return Config{}, err
	}
	if cfg.Retention5m, err = durationEnv("TARISYA_RETENTION_5M", cfg.Retention5m); err != nil {
		return Config{}, err
	}
	if cfg.RetentionAggregated, err = durationEnv("TARISYA_RETENTION_AGGREGATED", cfg.RetentionAggregated); err != nil {
		return Config{}, err
	}
	if cfg.RetentionRaw <= 0 || cfg.Retention5m <= cfg.RetentionRaw || cfg.RetentionAggregated <= cfg.Retention5m {
		return Config{}, errors.New("retention must satisfy 0 < TARISYA_RETENTION_RAW < TARISYA_RETENTION_5M < TARISYA_RETENTION_AGGREGATED")
	}
	if value := strings.TrimSpace(os.Getenv("TARISYA_MAX_DATABASE_SIZE")); value != "" {
		cfg.MaxDatabaseSize, err = parseByteSize(value)
		if err != nil {
			return Config{}, err
		}
	}
	if value := strings.TrimSpace(os.Getenv("TARISYA_DATABASE_WARNING_THRESHOLD")); value != "" {
		cfg.DatabaseWarningThreshold, err = strconv.ParseFloat(value, 64)
		if err != nil || cfg.DatabaseWarningThreshold <= 0 || cfg.DatabaseWarningThreshold > 1 {
			return Config{}, errors.New("TARISYA_DATABASE_WARNING_THRESHOLD must be a number between 0 and 1")
		}
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

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	if strings.HasSuffix(strings.ToLower(value), "d") {
		days, err := strconv.ParseInt(strings.TrimSpace(value[:len(value)-1]), 10, 64)
		if err == nil && days > 0 {
			return time.Duration(days) * 24 * time.Hour, nil
		}
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return d, nil
}

func parseByteSize(value string) (int64, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	for _, unit := range []struct {
		suffix     string
		multiplier int64
	}{{"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"B", 1}} {
		suffix, multiplier := unit.suffix, unit.multiplier
		if strings.HasSuffix(value, suffix) {
			number := strings.TrimSpace(strings.TrimSuffix(value, suffix))
			n, err := strconv.ParseInt(number, 10, 64)
			if err != nil || n <= 0 {
				break
			}
			return n * multiplier, nil
		}
	}
	return 0, errors.New("TARISYA_MAX_DATABASE_SIZE must be a positive size such as 5GB")
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
