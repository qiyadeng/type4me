package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// Mode determines whether the receiver listens on a port (LAN mode) or
// subscribes to a relay (relay mode).
type Mode string

const (
	ModeListener        Mode = "listener"
	ModeRelaySubscriber Mode = "relay-subscriber"
)

// Config holds the receiver's runtime configuration.
//
// Sources, in priority order:
//  1. Environment variables (TYPE4ME_MODE, TYPE4ME_PORT, ... TYPE4ME_RELAY_URL, ...)
//  2. Config file JSON
//  3. Defaults
//
// Token: in listener mode, if absent in both env and file, one is generated
// (32 random bytes, base64url-encoded) and persisted to the file on next Save.
// In relay-subscriber mode, the listener token is NOT generated; instead
// relay_url / device_id / device_token are required.
type Config struct {
	Mode Mode `json:"mode"`

	// Listener mode fields (LAN, existing S0+S1+S3 behavior):
	Port     int    `json:"port"`
	BindAddr string `json:"bind_addr"`
	Name     string `json:"name"`
	Token    string `json:"token"`

	// Relay-subscriber mode fields:
	RelayURL    string `json:"relay_url"`
	DeviceID    string `json:"device_id"`
	DeviceToken string `json:"device_token"`
}

const (
	DefaultPort     = 47318
	DefaultBindAddr = "0.0.0.0"
)

// Load reads config from the given file path, applies env overrides, and
// generates a listener-mode token if missing.
func Load(path string) (*Config, error) {
	cfg := &Config{
		Mode:     ModeListener,
		Port:     DefaultPort,
		BindAddr: DefaultBindAddr,
		Name:     hostname(),
	}

	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, cfg)
	}

	if v := os.Getenv("TYPE4ME_MODE"); v != "" {
		cfg.Mode = Mode(v)
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeListener
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
	if v := os.Getenv("TYPE4ME_RELAY_URL"); v != "" {
		cfg.RelayURL = v
	}
	if v := os.Getenv("TYPE4ME_DEVICE_ID"); v != "" {
		cfg.DeviceID = v
	}
	if v := os.Getenv("TYPE4ME_DEVICE_TOKEN"); v != "" {
		cfg.DeviceToken = v
	}

	// Auto-generate listener token only when in listener mode and missing.
	if cfg.Mode == ModeListener && cfg.Token == "" {
		tok, err := generateToken()
		if err != nil {
			return nil, fmt.Errorf("generate token: %w", err)
		}
		cfg.Token = tok
	}

	if cfg.Mode == ModeRelaySubscriber {
		if cfg.RelayURL == "" || cfg.DeviceID == "" || cfg.DeviceToken == "" {
			return nil, fmt.Errorf("mode=relay-subscriber requires relay_url, device_id, device_token")
		}
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
