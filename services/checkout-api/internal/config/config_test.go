package config

import (
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

var configKeys = []string{
	"HTTP_ADDR",
	"LOG_LEVEL",
	"READ_HEADER_TIMEOUT",
	"READ_TIMEOUT",
	"WRITE_TIMEOUT",
	"IDLE_TIMEOUT",
	"SHUTDOWN_TIMEOUT",
	"MAX_BODY_BYTES",
	"IDEMPOTENCY_TTL",
	"FAULT_LATENCY_MS",
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range configKeys {
		value, wasSet := os.LookupEnv(key)
		if wasSet {
			t.Cleanup(func() { _ = os.Setenv(key, value) })
		} else {
			t.Cleanup(func() { _ = os.Unsetenv(key) })
		}
		_ = os.Unsetenv(key)
	}
}

func TestLoadDefaults(t *testing.T) {
	clearConfigEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", cfg.Addr)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
	if cfg.MaxBodyBytes != 1<<20 {
		t.Errorf("MaxBodyBytes = %d, want 1MiB", cfg.MaxBodyBytes)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 15s", cfg.ShutdownTimeout)
	}
	if cfg.IdempotencyTTL != 24*time.Hour {
		t.Errorf("IdempotencyTTL = %v, want 24h", cfg.IdempotencyTTL)
	}
	if cfg.FaultLatency != 0 {
		t.Errorf("FaultLatency = %v, want 0", cfg.FaultLatency)
	}
}

func TestLoadOverrides(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("HTTP_ADDR", "127.0.0.1:9090")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("MAX_BODY_BYTES", "2048")
	t.Setenv("FAULT_LATENCY_MS", "250")
	t.Setenv("SHUTDOWN_TIMEOUT", "3s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != "127.0.0.1:9090" {
		t.Errorf("Addr = %q", cfg.Addr)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v", cfg.LogLevel)
	}
	if cfg.MaxBodyBytes != 2048 {
		t.Errorf("MaxBodyBytes = %d", cfg.MaxBodyBytes)
	}
	if cfg.FaultLatency != 250*time.Millisecond {
		t.Errorf("FaultLatency = %v", cfg.FaultLatency)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "unknown log level", key: "LOG_LEVEL", value: "verbose"},
		{name: "bad duration", key: "READ_TIMEOUT", value: "soon"},
		{name: "zero body limit", key: "MAX_BODY_BYTES", value: "0"},
		{name: "fault latency too high", key: "FAULT_LATENCY_MS", value: "6000"},
		{name: "negative shutdown", key: "SHUTDOWN_TIMEOUT", value: "-1s"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(tc.key, tc.value)

			_, err := Load()
			if err == nil {
				t.Fatalf("expected error for %s=%s", tc.key, tc.value)
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Errorf("error %q does not mention %s", err, tc.key)
			}
		})
	}
}
