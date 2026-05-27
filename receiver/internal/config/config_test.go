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
