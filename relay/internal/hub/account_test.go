package hub

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestRegisterUserAndAuthenticate(t *testing.T) {
	h := newTestHub(t)
	acc, err := h.RegisterUser("Alice", "supersecret")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if acc.Username != "Alice" || acc.PasswordHash == "" {
		t.Fatalf("bad account: %+v", acc)
	}
	got, err := h.Authenticate("Alice", "supersecret")
	if err != nil || got.ID != acc.ID {
		t.Fatalf("authenticate: got %+v err %v", got, err)
	}
	if _, err := h.Authenticate("Alice", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: got %v", err)
	}
	if _, err := h.Authenticate("ghost", "whatever"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown user: got %v", err)
	}
}

func TestRegisterUserValidation(t *testing.T) {
	h := newTestHub(t)
	if _, err := h.RegisterUser("ab", "supersecret"); !errors.Is(err, ErrUsernameInvalid) {
		t.Errorf("short username: got %v", err)
	}
	if _, err := h.RegisterUser(strings.Repeat("a", 33), "supersecret"); !errors.Is(err, ErrUsernameInvalid) {
		t.Errorf("too-long username: got %v", err)
	}
	if _, err := h.RegisterUser("alice", "short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("short password: got %v", err)
	}
	if _, err := h.RegisterUser("carol", strings.Repeat("p", 73)); !errors.Is(err, ErrPasswordTooLong) {
		t.Errorf("too-long password: got %v", err)
	}
	if _, err := h.RegisterUser("Alice", "supersecret"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.RegisterUser("alice", "supersecret"); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("dup username: got %v", err)
	}
}

func TestListDevicesByAccountIsolation(t *testing.T) {
	h := newTestHub(t)
	a1, _ := h.RegisterUser("alice", "supersecret")
	a2, _ := h.RegisterUser("bobby", "supersecret")
	_, _, _ = h.AddDevice(a1.ID, "Mac", RoleEither)
	_, _, _ = h.AddDevice(a1.ID, "Win", RoleEither)
	_, _, _ = h.AddDevice(a2.ID, "Other", RoleEither)
	if got := len(h.ListDevicesByAccount(a1.ID)); got != 2 {
		t.Errorf("a1 devices = %d, want 2", got)
	}
	if got := len(h.ListDevicesByAccount(a2.ID)); got != 1 {
		t.Errorf("a2 devices = %d, want 1", got)
	}
}

func TestIsOnline(t *testing.T) {
	h := newTestHub(t)
	a, _ := h.RegisterUser("alice", "supersecret")
	dev, _, _ := h.AddDevice(a.ID, "Win", RoleEither)
	if h.IsOnline(dev.ID) {
		t.Error("should be offline before subscribe")
	}
	_, unsub := h.Subscribe(dev.ID)
	defer unsub()
	if !h.IsOnline(dev.ID) {
		t.Error("should be online after subscribe")
	}
}
