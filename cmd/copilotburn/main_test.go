package main

import (
	"os"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	if cfg.Redis.Host != "localhost" {
		t.Errorf("expected default host localhost, got %s", cfg.Redis.Host)
	}
	if cfg.Redis.Port != 6379 {
		t.Errorf("expected default port 6379, got %d", cfg.Redis.Port)
	}
	if cfg.Redis.KeyPrefix != "copilot-burn:" {
		t.Errorf("expected default key_prefix copilot-burn:, got %s", cfg.Redis.KeyPrefix)
	}
	if cfg.Redis.TTLDays != 90 {
		t.Errorf("expected default ttl_days 90, got %d", cfg.Redis.TTLDays)
	}
	if cfg.Poppit.ListName != "poppit:notifications" {
		t.Errorf("expected default list_name poppit:notifications, got %s", cfg.Poppit.ListName)
	}
	if cfg.Poppit.OutputChannel != "poppit:command-output" {
		t.Errorf("expected default output_channel poppit:command-output, got %s", cfg.Poppit.OutputChannel)
	}
}

func TestLoadConfig_EnvOverrides(t *testing.T) {
	os.Setenv("REDIS_KEY_PREFIX", "custom-burn:")
	os.Setenv("POPPIT_SERVICE_REDIS_LIST_NAME", "custom-notifications")
	defer func() {
		os.Unsetenv("REDIS_KEY_PREFIX")
		os.Unsetenv("POPPIT_SERVICE_REDIS_LIST_NAME")
	}()

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	if cfg.Redis.KeyPrefix != "custom-burn:" {
		t.Errorf("expected key prefix custom-burn:, got %s", cfg.Redis.KeyPrefix)
	}
	if cfg.Poppit.ListName != "custom-notifications" {
		t.Errorf("expected list name custom-notifications, got %s", cfg.Poppit.ListName)
	}
}
