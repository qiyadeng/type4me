package hub

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"sync"
	"time"
)

type Hub struct {
	mu        sync.RWMutex
	statePath string
	accounts  map[string]*Account
	devices   map[string]*Device
	subs      map[string]chan *Message
	cache     *tokenCache
}

// New creates a Hub, loading existing state from disk if present.
func New(statePath string) (*Hub, error) {
	st, err := loadState(statePath)
	if err != nil {
		return nil, err
	}
	h := &Hub{
		statePath: statePath,
		accounts:  map[string]*Account{},
		devices:   map[string]*Device{},
		subs:      map[string]chan *Message{},
		cache:     newTokenCache(),
	}
	for _, a := range st.Accounts {
		h.accounts[a.ID] = a
	}
	for _, d := range st.Devices {
		h.devices[d.ID] = d
	}
	return h, nil
}

func (h *Hub) AddAccount(name string) (*Account, error) {
	if strings.TrimSpace(name) == "" {
		return nil, ErrAccountNameRequired
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	acc := &Account{
		ID:        "acct-" + shortID(),
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}
	h.accounts[acc.ID] = acc
	if err := h.persistLocked(); err != nil {
		delete(h.accounts, acc.ID)
		return nil, err
	}
	return acc, nil
}

func (h *Hub) ListAccounts() []*Account {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Account, 0, len(h.accounts))
	for _, a := range h.accounts {
		out = append(out, a)
	}
	return out
}

// persistLocked writes current state to disk; caller holds h.mu.
func (h *Hub) persistLocked() error {
	st := &State{
		Version:  stateVersion,
		Accounts: make([]*Account, 0, len(h.accounts)),
		Devices:  make([]*Device, 0, len(h.devices)),
	}
	for _, a := range h.accounts {
		st.Accounts = append(st.Accounts, a)
	}
	for _, d := range h.devices {
		st.Devices = append(st.Devices, d)
	}
	return saveState(h.statePath, st)
}

// shortID returns a URL-safe 10-character random ID.
func shortID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:10]
}
