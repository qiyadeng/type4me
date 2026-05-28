package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	data, _ := json.Marshal(map[string]any{"port": 9999, "bind_addr": "0.0.0.0", "name": "from-file"})
	os.WriteFile(cfgFile, data, 0600)

	t.Setenv("TYPE4ME_PORT", "47318")
	t.Setenv("TYPE4ME_TOKEN", "env-token")
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 47318 {
		t.Errorf("env did not override port: got %d", cfg.Port)
	}
	if cfg.Token != "env-token" {
		t.Errorf("token from env not picked up: got %q", cfg.Token)
	}
	if cfg.Name != "from-file" {
		t.Errorf("name from file lost: got %q", cfg.Name)
	}
}

func TestLoadGeneratesTokenWhenMissing(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	os.WriteFile(cfgFile, []byte("{}"), 0600)
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Token) < 32 {
		t.Errorf("expected generated token >=32 chars, got %d", len(cfg.Token))
	}
}

func TestSavePersistsNonSecretFields(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	os.WriteFile(cfgFile, []byte("{}"), 0600)
	cfg, _ := Load(cfgFile)
	cfg.Name = "Test"
	if err := cfg.Save(cfgFile); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cfg2, _ := Load(cfgFile)
	if cfg2.Name != "Test" {
		t.Errorf("Save didn't persist Name: got %q", cfg2.Name)
	}
}

func TestLoadDefaultsToListenerMode(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	os.WriteFile(cfgFile, []byte("{}"), 0600)
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != ModeListener {
		t.Errorf("mode = %q, want listener", cfg.Mode)
	}
}

func TestLoadRelaySubscriberRequiresFields(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	os.WriteFile(cfgFile, []byte(`{"mode":"relay-subscriber"}`), 0600)
	_, err := Load(cfgFile)
	if err == nil {
		t.Error("expected error for missing relay fields, got nil")
	}
}

func TestLoadRelaySubscriberFromEnv(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	os.WriteFile(cfgFile, []byte("{}"), 0600)
	t.Setenv("TYPE4ME_MODE", "relay-subscriber")
	t.Setenv("TYPE4ME_RELAY_URL", "https://relay.example.com")
	t.Setenv("TYPE4ME_DEVICE_ID", "dev-Win")
	t.Setenv("TYPE4ME_DEVICE_TOKEN", "tok-win")
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != ModeRelaySubscriber {
		t.Errorf("mode = %q", cfg.Mode)
	}
	if cfg.RelayURL != "https://relay.example.com" || cfg.DeviceID != "dev-Win" {
		t.Errorf("relay fields wrong: %+v", cfg)
	}
}

func TestLoadListenerModeStillGeneratesToken(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	os.WriteFile(cfgFile, []byte("{}"), 0600)
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != ModeListener {
		t.Fatal("not listener mode")
	}
	if len(cfg.Token) < 32 {
		t.Errorf("token len=%d", len(cfg.Token))
	}
}

func TestLoadRelayModeDoesNotGenerateListenerToken(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	os.WriteFile(cfgFile, []byte(`{"mode":"relay-subscriber","relay_url":"x","device_id":"y","device_token":"z"}`), 0600)
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "" {
		t.Errorf("listener token should be empty in relay mode, got %q", cfg.Token)
	}
}
