package config_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yaConversationWriter/internal/config"
)

func TestYAMLSourceAppliesDefaults(t *testing.T) {
	path := writeConfig(t, "{}\n")

	cfg, err := config.NewYAMLSource(path, nil).Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.App.ShutdownTimeout != 10*time.Second {
		t.Fatalf("ShutdownTimeout = %v", cfg.App.ShutdownTimeout)
	}
	if cfg.Workers.Count != 2 || cfg.Workers.QueueSize != 32 || cfg.Workers.TaskTimeout != 2*time.Minute {
		t.Fatalf("unexpected worker defaults: %+v", cfg.Workers)
	}
	if cfg.Storage.Provider != config.ProviderMemory || cfg.Speech.Provider != config.ProviderMock || cfg.LLM.Provider != config.ProviderMock {
		t.Fatalf("unexpected provider defaults: storage=%q speech=%q llm=%q", cfg.Storage.Provider, cfg.Speech.Provider, cfg.LLM.Provider)
	}
}

func TestYAMLSourceLoadsListenersAndSecrets(t *testing.T) {
	path := writeConfig(t, `
listeners:
  - name: first
    type: telegram
    enabled: true
    token_env: FIRST_TOKEN
  - name: second
    type: telegram
    enabled: false
`)
	lookup := func(key string) (string, bool) {
		if key == "FIRST_TOKEN" {
			return "token-value", true
		}
		return "", false
	}

	cfg, err := config.NewYAMLSource(path, lookup).Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Listeners) != 2 {
		t.Fatalf("listeners count = %d", len(cfg.Listeners))
	}
	if cfg.Listeners[0].Token != "token-value" {
		t.Fatal("enabled listener secret was not resolved")
	}
	if cfg.Listeners[1].Token != "" {
		t.Fatal("disabled listener unexpectedly resolved a secret")
	}
}

func TestYAMLSourceLoadsPostgresConfigurationAndSecret(t *testing.T) {
	path := writeConfig(t, `
storage:
  provider: postgres
  postgres:
    host: database.internal
    port: 5433
    database: meetings
    user: app
    password_env: DATABASE_PASSWORD
    ssl_mode: require
    min_connections: 1
    max_connections: 8
    connect_timeout: 3s
`)
	cfg, err := config.NewYAMLSource(path, func(key string) (string, bool) {
		if key == "DATABASE_PASSWORD" {
			return "database-secret", true
		}
		return "", false
	}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	postgres := cfg.Storage.Postgres
	if cfg.Storage.Provider != config.ProviderPostgres || postgres.Host != "database.internal" || postgres.Port != 5433 {
		t.Fatalf("unexpected PostgreSQL config: %+v", postgres)
	}
	if postgres.Password != "database-secret" || postgres.ConnectTimeout != 3*time.Second {
		t.Fatal("PostgreSQL secret or duration was not resolved")
	}
}

func TestYAMLSourceRejectsMissingPostgresSecret(t *testing.T) {
	path := writeConfig(t, `
storage:
  provider: postgres
  postgres:
    password_env: DATABASE_PASSWORD
`)
	_, err := config.NewYAMLSource(path, func(string) (string, bool) { return "", false }).Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "DATABASE_PASSWORD") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestYAMLSourceRejectsMissingSecretWithoutLeakingValues(t *testing.T) {
	path := writeConfig(t, `
listeners:
  - name: primary
    type: telegram
    enabled: true
    token_env: BOT_TOKEN
`)

	_, err := config.NewYAMLSource(path, func(string) (string, bool) { return "", false }).Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "BOT_TOKEN") {
		t.Fatalf("Load() error = %v", err)
	}

	secret := "do-not-print-this-secret"
	invalidPath := writeConfig(t, "storage:\n  provider: postgres\n  postgres:\n    max_connections: 0\n")
	_, err = config.NewYAMLSource(invalidPath, func(string) (string, bool) { return secret, true }).Load(context.Background())
	if err == nil {
		t.Fatal("Load() error = nil")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked a secret: %v", err)
	}
}

func TestYAMLSourceRejectsInvalidConfiguration(t *testing.T) {
	tests := map[string]string{
		"unknown field":     "unknown: true\n",
		"unknown listener":  "listeners:\n  - name: x\n    type: http\n    enabled: false\n",
		"unknown provider":  "speech:\n  provider: real\n",
		"bad duration":      "app:\n  shutdown_timeout: soon\n",
		"bad limit":         "workers:\n  count: 0\n",
		"bad postgres pool": "storage:\n  provider: postgres\n  postgres:\n    min_connections: 2\n    max_connections: 1\n",
	}

	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := config.NewYAMLSource(writeConfig(t, contents), func(string) (string, bool) { return "test-secret", true }).Load(context.Background())
			if err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}

func TestYAMLSourceHandlesMissingFileAndCancellation(t *testing.T) {
	_, err := config.NewYAMLSource(filepath.Join(t.TempDir(), "missing.yaml"), nil).Load(context.Background())
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = config.NewYAMLSource("unused", nil).Load(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Load() error = %v", err)
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
