package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// Config holds the receiver's runtime configuration.
//
// Sources, in priority order:
//  1. Environment variables (TYPE4ME_PORT, TYPE4ME_TOKEN, TYPE4ME_BIND_ADDR, TYPE4ME_NAME)
//  2. Config file JSON
//  3. Defaults
//
// Token: if absent in both env and file, one is generated (32 random bytes,
// base64url-encoded) and persisted to the file on next Save.
type Config struct {
	Port     int    `json:"port"`
	BindAddr string `json:"bind_addr"`
	Name     string `json:"name"`
	Token    string `json:"token"` // S1: stored in file; S2+ moves to Keychain.
}

const (
	DefaultPort     = 47318
	DefaultBindAddr = "0.0.0.0"
)

// Load reads config from the given file path, applies env overrides, and
// generates a token if missing. Save() must be called to persist a generated token.
func Load(path string) (*Config, error) {
	cfg := &Config{
		Port:     DefaultPort,
		BindAddr: DefaultBindAddr,
		Name:     hostname(),
	}

	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, cfg) // best effort; ignore parse errors
	}

	if v := os.Getenv("TYPE4ME_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Port = n
		}
	}
	if v := os.Getenv("TYPE4ME_BIND_ADDR"); v != "" {
		cfg.BindAddr = v
	}
	if v := os.Getenv("TYPE4ME_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("TYPE4ME_NAME"); v != "" {
		cfg.Name = v
	}

	if cfg.Token == "" {
		tok, err := generateToken()
		if err != nil {
			return nil, fmt.Errorf("generate token: %w", err)
		}
		cfg.Token = tok
	}
	return cfg, nil
}

func (c *Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hostname() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "type4me-receiver"
}
