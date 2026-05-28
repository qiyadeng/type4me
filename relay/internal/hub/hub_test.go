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
