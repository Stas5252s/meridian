package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration. Everything is optional and has a
// production-safe default so the binary can start with an empty environment.
type Config struct {
	Addr              string
	LogLevel          slog.Level
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxBodyBytes      int64
	IdempotencyTTL    time.Duration
	FaultLatency      time.Duration
	RedisAddr         string
	RedisPassword     string
	RedisDB           int
}

// Load reads and validates configuration from the environment.
func Load() (Config, error) {
	var cfg Config

	cfg.Addr = strings.TrimSpace(getenv("HTTP_ADDR", ":8080"))
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}

	level, err := parseLevel(getenv("LOG_LEVEL", "info"))
	if err != nil {
		return Config{}, err
	}
	cfg.LogLevel = level

	if cfg.ReadHeaderTimeout, err = envDuration("READ_HEADER_TIMEOUT", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ReadTimeout, err = envDuration("READ_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WriteTimeout, err = envDuration("WRITE_TIMEOUT", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.IdleTimeout, err = envDuration("IDLE_TIMEOUT", 120*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = envDuration("SHUTDOWN_TIMEOUT", 15*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.IdempotencyTTL, err = envDuration("IDEMPOTENCY_TTL", 24*time.Hour); err != nil {
		return Config{}, err
	}

	cfg.MaxBodyBytes, err = envInt64("MAX_BODY_BYTES", 1<<20)
	if err != nil {
		return Config{}, err
	}
	if cfg.MaxBodyBytes < 1 {
		return Config{}, fmt.Errorf("MAX_BODY_BYTES must be positive")
	}

	cfg.RedisAddr = strings.TrimSpace(getenv("REDIS_ADDR", ""))
	cfg.RedisPassword = os.Getenv("REDIS_PASSWORD")
	redisDB, err := envInt64("REDIS_DB", 0)
	if err != nil {
		return Config{}, err
	}
	if redisDB < 0 || redisDB > 15 {
		return Config{}, fmt.Errorf("REDIS_DB must be between 0 and 15")
	}
	cfg.RedisDB = int(redisDB)

	latencyMS, err := envInt64("FAULT_LATENCY_MS", 0)
	if err != nil {
		return Config{}, err
	}
	if latencyMS < 0 || latencyMS > 5000 {
		return Config{}, fmt.Errorf("FAULT_LATENCY_MS must be between 0 and 5000")
	}
	cfg.FaultLatency = time.Duration(latencyMS) * time.Millisecond

	for name, d := range map[string]time.Duration{
		"READ_HEADER_TIMEOUT": cfg.ReadHeaderTimeout,
		"READ_TIMEOUT":        cfg.ReadTimeout,
		"WRITE_TIMEOUT":       cfg.WriteTimeout,
		"IDLE_TIMEOUT":        cfg.IdleTimeout,
		"SHUTDOWN_TIMEOUT":    cfg.ShutdownTimeout,
		"IDEMPOTENCY_TTL":     cfg.IdempotencyTTL,
	} {
		if d <= 0 {
			return Config{}, fmt.Errorf("%s must be greater than zero", name)
		}
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}

func envInt64(key string, fallback int64) (int64, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func parseLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("LOG_LEVEL: unsupported level %q", raw)
	}
}
