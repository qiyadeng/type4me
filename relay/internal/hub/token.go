package hub

import (
	"crypto/rand"
	"encoding/base64"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// generateToken returns 32 random bytes base64url-encoded (43 chars).
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	return string(h), err
}

func verifyToken(token, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)) == nil
}

// tokenCache memoizes token -> deviceID lookups so /dispatch hot path doesn't
// run bcrypt every request. Entries are invalidated on rotate / delete.
type tokenCache struct {
	mu sync.RWMutex
	m  map[string]string // token -> deviceID
	r  map[string]string // deviceID -> token (reverse, for invalidate)
}

func newTokenCache() *tokenCache {
	return &tokenCache{m: map[string]string{}, r: map[string]string{}}
}

func (c *tokenCache) get(token string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	d, ok := c.m[token]
	return d, ok
}

func (c *tokenCache) put(token, deviceID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// clear any stale forward entries for this device
	if oldTok, ok := c.r[deviceID]; ok {
		delete(c.m, oldTok)
	}
	c.m[token] = deviceID
	c.r[deviceID] = token
}

func (c *tokenCache) invalidate(deviceID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if tok, ok := c.r[deviceID]; ok {
		delete(c.m, tok)
		delete(c.r, deviceID)
	}
}

// hashPassword / verifyPassword are semantic aliases for bcrypt in the password context.
func hashPassword(password string) (string, error) { return hashToken(password) }
func verifyPassword(password, hash string) bool     { return verifyToken(password, hash) }
