package config

import (
	"log/slog"
	"testing"
	"time"

	"github.com/caarlos0/env/v11"
)

func TestLoadUsesSafeDefaults(t *testing.T) {
	cfg, err := load(env.Options{Environment: map[string]string{}})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Env != "local" {
		t.Fatalf("Env = %q, want local", cfg.Env)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("LogLevel = %v, want info", cfg.LogLevel)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Fatalf("HTTP.Addr = %q, want :8080", cfg.HTTP.Addr)
	}
	if cfg.NATS.URL != "nats://localhost:4222" {
		t.Fatalf("NATS.URL = %q, want default localhost URL", cfg.NATS.URL)
	}
	if cfg.Database.MaxConns != 10 {
		t.Fatalf("Database.MaxConns = %d, want 10", cfg.Database.MaxConns)
	}
	if cfg.OpenStack.Timeout != 20*time.Second {
		t.Fatalf("OpenStack.Timeout = %v, want 20s", cfg.OpenStack.Timeout)
	}
	if cfg.OpenStack.Configured() {
		t.Fatal("OpenStack must not be configured without credentials")
	}
	if cfg.KI.Configured() {
		t.Fatal("KI must not be configured without token, session or credentials")
	}
	if cfg.KI.SessionID != "1" {
		t.Fatalf("KI.SessionID = %q, want 1", cfg.KI.SessionID)
	}
	if cfg.Capacity.ThresholdPercent != 90 {
		t.Fatalf("Capacity.ThresholdPercent = %v, want 90", cfg.Capacity.ThresholdPercent)
	}
	if cfg.Capacity.DemoVCPUs != 128 {
		t.Fatalf("Capacity.DemoVCPUs = %d, want 128", cfg.Capacity.DemoVCPUs)
	}
}

func TestKIConfiguredSupportsProjectCredentials(t *testing.T) {
	cfg, err := load(env.Options{Environment: map[string]string{
		"KI_PROJECT_ID": "project-1",
		"KI_USERNAME":   "student",
		"KI_PASSWORD":   "secret",
	}})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.KI.Configured() {
		t.Fatal("KI should be configured with project id and credentials")
	}

	cfg.KI.Password = ""
	if cfg.KI.Configured() {
		t.Fatal("KI project session auth must require password")
	}
}

func TestLoadRejectsEmptyRequiredEndpoints(t *testing.T) {
	_, err := load(env.Options{Environment: map[string]string{
		"HTTP_ADDR": " ",
		"NATS_URL":  "nats://localhost:4222",
	}})
	if err == nil {
		t.Fatal("expected HTTP_ADDR validation error")
	}

	_, err = load(env.Options{Environment: map[string]string{
		"HTTP_ADDR": ":8080",
		"NATS_URL":  " ",
	}})
	if err == nil {
		t.Fatal("expected NATS_URL validation error")
	}
}

func TestOpenStackConfiguredRequiresProjectScope(t *testing.T) {
	cfg, err := load(env.Options{Environment: map[string]string{
		"OS_USERNAME":     "student",
		"OS_PASSWORD":     "secret",
		"OS_PROJECT_NAME": "course-project",
	}})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.OpenStack.Configured() {
		t.Fatal("OpenStack should be configured with auth URL, credentials and project name")
	}

	cfg.OpenStack.ProjectName = ""
	cfg.OpenStack.ProjectID = ""
	if cfg.OpenStack.Configured() {
		t.Fatal("OpenStack must require project name or project id")
	}
}
