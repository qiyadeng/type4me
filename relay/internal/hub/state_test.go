package hub

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	now := time.Now().UTC().Truncate(time.Second)
	want := &State{
		Version: 1,
		Accounts: []*Account{
			{ID: "acct-1", Name: "test", CreatedAt: now},
		},
		Devices: []*Device{
			{ID: "dev-1", AccountID: "acct-1", Label: "Mac",
				Role: RoleEither, TokenHash: "$2a$10$xxx",
				CreatedAt: now, LastSeen: now},
		},
	}
	if err := saveState(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Accounts) != 1 || got.Accounts[0].Name != "test" {
		t.Errorf("accounts roundtrip failed: %+v", got.Accounts)
	}
	if len(got.Devices) != 1 || got.Devices[0].TokenHash != "$2a$10$xxx" {
		t.Errorf("devices roundtrip failed: %+v", got.Devices)
	}
}

func TestLoadStateMissingFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")
	st, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Accounts) != 0 || len(st.Devices) != 0 {
		t.Errorf("expected empty state, got %+v", st)
	}
	if st.Version != 1 {
		t.Errorf("expected version 1, got %d", st.Version)
	}
}

func TestSaveStateAtomicReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	_ = os.WriteFile(path, []byte("{}"), 0600)
	st := &State{Version: 1, Accounts: []*Account{{ID: "x", Name: "y"}}}
	if err := saveState(path, st); err != nil {
		t.Fatal(err)
	}
	// no .tmp leftover
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp leaked: %v", err)
	}
	got, _ := loadState(path)
	if got.Accounts[0].ID != "x" {
		t.Errorf("not overwritten: %+v", got)
	}
}

func TestSaveStatePermissions0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	_ = saveState(path, &State{Version: 1})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %o, want 0600", info.Mode().Perm())
	}
}
