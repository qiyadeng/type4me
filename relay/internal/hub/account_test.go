package hub

import (
	"os"
	"path/filepath"
	"testing"
)

// Simulate a v1 state.json: account has no username/password_hash fields.
func TestLoadLegacyV1StateNoUsername(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	legacy := `{"version":1,"accounts":[{"id":"acct-legacy","name":"Old","created_at":"2024-01-01T00:00:00Z"}],"devices":[]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(h.ListAccounts()) != 1 {
		t.Fatalf("legacy account not loaded")
	}
	// Empty username must not be added to the username index.
	if len(h.usernames) != 0 {
		t.Errorf("usernames index = %d, want 0 (legacy account has no username)", len(h.usernames))
	}
}

func TestLoadV2StatePopulatesUsernameIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	v2 := `{"version":2,"accounts":[{"id":"acct-x","name":"Alice","username":"Alice","password_hash":"x","created_at":"2024-01-01T00:00:00Z"}],"devices":[]}`
	if err := os.WriteFile(path, []byte(v2), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := h.usernames["alice"]; got != "acct-x" {
		t.Errorf("usernames[\"alice\"] = %q, want acct-x (case-insensitive index)", got)
	}
}
