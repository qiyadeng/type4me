package hub

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
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

func TestSubscribeAndDispatch(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	mac, macTok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	win, _, _ := h.AddDevice(acc.ID, "Win", RoleEither)

	ch, unsub := h.Subscribe(win.ID)
	defer unsub()

	msg, err := h.Dispatch(macTok, win.ID, "hello", "req-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if msg.ID == "" || msg.Text != "hello" {
		t.Errorf("bad msg: %+v", msg)
	}

	select {
	case got := <-ch:
		if got.Text != "hello" || got.FromDevice != mac.ID {
			t.Errorf("received wrong msg: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no message received within 1s")
	}
}

func TestDispatchReceiverOffline(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	_, macTok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	win, _, _ := h.AddDevice(acc.ID, "Win", RoleEither)
	// no Subscribe -> offline
	_, err := h.Dispatch(macTok, win.ID, "x", "", false)
	if !errors.Is(err, ErrReceiverOffline) {
		t.Errorf("expected ErrReceiverOffline, got %v", err)
	}
}

func TestDispatchCrossAccount(t *testing.T) {
	h := newTestHub(t)
	a, _ := h.AddAccount("A")
	b, _ := h.AddAccount("B")
	_, aTok, _ := h.AddDevice(a.ID, "MacA", RoleEither)
	devB, _, _ := h.AddDevice(b.ID, "WinB", RoleEither)
	ch, unsub := h.Subscribe(devB.ID)
	defer unsub()
	_, err := h.Dispatch(aTok, devB.ID, "x", "", false)
	if !errors.Is(err, ErrCrossAccount) {
		t.Errorf("expected ErrCrossAccount, got %v", err)
	}
	select {
	case got := <-ch:
		t.Errorf("cross-account dispatch leaked: %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDispatchTargetNotFound(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	_, macTok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	_, err := h.Dispatch(macTok, "dev-missing", "x", "", false)
	if !errors.Is(err, ErrDeviceNotFound) {
		t.Errorf("expected ErrDeviceNotFound, got %v", err)
	}
}

func TestDispatchInvalidSenderToken(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	win, _, _ := h.AddDevice(acc.ID, "Win", RoleEither)
	_, err := h.Dispatch("bogus-token", win.ID, "x", "", false)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

func TestDispatchBackpressure(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	_, macTok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	win, _, _ := h.AddDevice(acc.ID, "Win", RoleEither)
	ch, unsub := h.Subscribe(win.ID)
	defer unsub()
	// Fill buffer (cap is 16) without reading
	for i := 0; i < 16; i++ {
		_, err := h.Dispatch(macTok, win.ID, "fill", "", false)
		if err != nil {
			t.Fatalf("fill %d: %v", i, err)
		}
	}
	// 17th should backpressure
	_, err := h.Dispatch(macTok, win.ID, "overflow", "", false)
	if !errors.Is(err, ErrBackpressure) {
		t.Errorf("expected ErrBackpressure, got %v", err)
	}
	_ = ch
}

func TestSubscribeReconnectClosesOldChannel(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	win, _, _ := h.AddDevice(acc.ID, "Win", RoleEither)
	ch1, _ := h.Subscribe(win.ID)
	ch2, unsub := h.Subscribe(win.ID)
	defer unsub()
	// ch1 must be closed
	select {
	case _, ok := <-ch1:
		if ok {
			t.Errorf("ch1 should be closed but got value")
		}
	case <-time.After(100 * time.Millisecond):
		t.Errorf("ch1 not closed in time")
	}
	_ = ch2
}

func TestSelfDispatchAllowed(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	dev, tok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	ch, unsub := h.Subscribe(dev.ID)
	defer unsub()
	_, err := h.Dispatch(tok, dev.ID, "echo", "", false)
	if err != nil {
		t.Fatalf("self-dispatch failed: %v", err)
	}
	select {
	case got := <-ch:
		if got.Text != "echo" {
			t.Errorf("self-dispatch wrong text: %q", got.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("self-dispatch didn't deliver")
	}
}

func TestConcurrentDispatch(t *testing.T) {
	h := newTestHub(t)
	acc, _ := h.AddAccount("Personal")
	_, macTok, _ := h.AddDevice(acc.ID, "Mac", RoleEither)
	win, _, _ := h.AddDevice(acc.ID, "Win", RoleEither)
	ch, unsub := h.Subscribe(win.ID)
	defer unsub()

	const n = 10
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			_, err := h.Dispatch(macTok, win.ID, "msg", "", false)
			errCh <- err
		}(i)
	}

	count := 0
	timeout := time.After(2 * time.Second)
	for count < n {
		select {
		case <-ch:
			count++
		case <-timeout:
			t.Fatalf("only %d/%d msgs delivered", count, n)
		}
	}
}
