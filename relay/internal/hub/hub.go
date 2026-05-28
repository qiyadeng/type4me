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

func (h *Hub) AddDevice(accountID, label string, role Role) (*Device, string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.accounts[accountID]; !ok {
		return nil, "", ErrAccountNotFound
	}
	tok, err := generateToken()
	if err != nil {
		return nil, "", err
	}
	hash, err := hashToken(tok)
	if err != nil {
		return nil, "", err
	}
	if role == "" {
		role = RoleEither
	}
	now := time.Now().UTC()
	dev := &Device{
		ID:        "dev-" + shortID(),
		AccountID: accountID,
		Label:     label,
		Role:      role,
		TokenHash: hash,
		CreatedAt: now,
		LastSeen:  now,
	}
	h.devices[dev.ID] = dev
	if err := h.persistLocked(); err != nil {
		delete(h.devices, dev.ID)
		return nil, "", err
	}
	h.cache.put(tok, dev.ID)
	return dev, tok, nil
}

func (h *Hub) ListDevices() []*Device {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Device, 0, len(h.devices))
	for _, d := range h.devices {
		out = append(out, d)
	}
	return out
}

func (h *Hub) GetDevice(id string) (*Device, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	d, ok := h.devices[id]
	if !ok {
		return nil, ErrDeviceNotFound
	}
	return d, nil
}

func (h *Hub) DeleteDevice(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.devices[id]; !ok {
		return ErrDeviceNotFound
	}
	delete(h.devices, id)
	h.cache.invalidate(id)
	if ch, ok := h.subs[id]; ok {
		close(ch)
		delete(h.subs, id)
	}
	return h.persistLocked()
}

func (h *Hub) RotateDeviceToken(id string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	dev, ok := h.devices[id]
	if !ok {
		return "", ErrDeviceNotFound
	}
	tok, err := generateToken()
	if err != nil {
		return "", err
	}
	hash, err := hashToken(tok)
	if err != nil {
		return "", err
	}
	dev.TokenHash = hash
	h.cache.invalidate(id)
	if err := h.persistLocked(); err != nil {
		return "", err
	}
	h.cache.put(tok, id)
	return tok, nil
}

// ResolveDeviceByToken returns the device matching `token`, using token cache
// to avoid bcrypt on every hit.
func (h *Hub) ResolveDeviceByToken(token string) (*Device, error) {
	if id, ok := h.cache.get(token); ok {
		h.mu.RLock()
		dev, ok := h.devices[id]
		h.mu.RUnlock()
		if ok && verifyToken(token, dev.TokenHash) {
			return dev, nil
		}
		// Stale cache (rotated/deleted) — fall through to full scan
		h.cache.invalidate(id)
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, dev := range h.devices {
		if verifyToken(token, dev.TokenHash) {
			h.cache.put(token, dev.ID)
			return dev, nil
		}
	}
	return nil, ErrInvalidToken
}
