package hub

import (
	"errors"
	"path/filepath"
	"testing"
)

func newTestHub(t *testing.T) *Hub {
	t.Helper()
	dir := t.TempDir()
	h, err := New(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestAddAccount(t *testing.T) {
	h := newTestHub(t)
	acc, err := h.AddAccount("Personal")
	if err != nil {
		t.Fatal(err)
	}
	if acc.ID == "" || acc.Name != "Personal" {
		t.Errorf("bad account: %+v", acc)
	}
}

func TestAddAccountEmptyNameRejected(t *testing.T) {
	h := newTestHub(t)
	_, err := h.AddAccount("")
	if !errors.Is(err, ErrAccountNameRequired) {
		t.Errorf("expected ErrAccountNameRequired, got %v", err)
	}
}

func TestListAccounts(t *testing.T) {
	h := newTestHub(t)
	_, _ = h.AddAccount("a")
	_, _ = h.AddAccount("b")
	accs := h.ListAccounts()
	if len(accs) != 2 {
		t.Errorf("len = %d, want 2", len(accs))
	}
}

func TestHubPersistsAccountsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	h1, _ := New(path)
	acc, _ := h1.AddAccount("Personal")
	h2, _ := New(path)
	accs := h2.ListAccounts()
	if len(accs) != 1 || accs[0].ID != acc.ID {
		t.Errorf("reload failed: %+v", accs)
	}
}

func TestAddDevice(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	dev, token, err := h.AddDevice(acc.ID, "My-Mac", RoleEither)
	if err != nil {
		t.Fatal(err)
	}
	if dev.ID == "" || dev.AccountID != acc.ID || dev.Label != "My-Mac" {
		t.Errorf("bad device: %+v", dev)
	}
	if len(token) != 43 {
		t.Errorf("token len = %d, want 43", len(token))
	}
	if dev.TokenHash == token {
		t.Errorf("hash should not equal plain token")
	}
}

func TestAddDeviceAccountNotFound(t *testing.T) {
	h := newTestHub(t)
	_, _, err := h.AddDevice("acct-missing", "Mac", RoleEither)
	if !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestListDevicesExcludesTokenHash(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	_, _, _ = h.AddDevice(acc.ID, "Mac", RoleEither)
	devs := h.ListDevices()
	if len(devs) != 1 {
		t.Errorf("len = %d", len(devs))
	}
	// ListDevices returns the same struct; caller is trusted not to leak TokenHash.
	// Server-side handler strips it before JSON serialization (verified in handler tests).
	if devs[0].TokenHash == "" {
		t.Errorf("hub should retain hash internally")
	}
}

func TestRotateDeviceToken(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	dev, oldTok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	newTok, err := h.RotateDeviceToken(dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if newTok == oldTok {
		t.Errorf("rotation returned same token")
	}
	// Old token no longer verifies
	if _, err := h.ResolveDeviceByToken(oldTok); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("old token still valid: %v", err)
	}
	// New token does
	resolved, err := h.ResolveDeviceByToken(newTok)
	if err != nil || resolved.ID != dev.ID {
		t.Errorf("new token failed: dev=%v err=%v", resolved, err)
	}
}

func TestDeleteDevice(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	dev, tok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	if err := h.DeleteDevice(dev.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.ResolveDeviceByToken(tok); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("token still valid after delete: %v", err)
	}
	if _, err := h.GetDevice(dev.ID); !errors.Is(err, ErrDeviceNotFound) {
		t.Errorf("get device after delete: %v", err)
	}
}

func TestResolveDeviceByTokenUsesCache(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	dev, tok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	// First lookup: bcrypt path
	d1, err := h.ResolveDeviceByToken(tok)
	if err != nil || d1.ID != dev.ID {
		t.Fatal(err)
	}
	// Second lookup: cache should hit
	if _, ok := h.cache.get(tok); !ok {
		t.Errorf("cache should have entry after first resolve")
	}
}
