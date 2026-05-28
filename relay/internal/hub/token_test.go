package hub

import (
	"strings"
	"testing"
)

func TestGenerateTokenLength(t *testing.T) {
	tok, err := generateToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 43 {
		t.Errorf("token len = %d, want 43 (32 bytes base64url)", len(tok))
	}
	tok2, _ := generateToken()
	if tok == tok2 {
		t.Errorf("two tokens equal: %q", tok)
	}
}

func TestHashAndVerifyToken(t *testing.T) {
	tok, _ := generateToken()
	hash, err := hashToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$2a$") && !strings.HasPrefix(hash, "$2b$") {
		t.Errorf("hash not bcrypt: %q", hash)
	}
	if !verifyToken(tok, hash) {
		t.Errorf("verify failed for correct token")
	}
	if verifyToken("wrong-token", hash) {
		t.Errorf("verify passed for wrong token")
	}
}

func TestTokenCacheHit(t *testing.T) {
	c := newTokenCache()
	tok, _ := generateToken()
	hash, _ := hashToken(tok)
	c.put(tok, "dev-1")
	got, ok := c.get(tok)
	if !ok || got != "dev-1" {
		t.Errorf("cache miss: ok=%v got=%q", ok, got)
	}
	_ = hash
}

func TestTokenCacheInvalidate(t *testing.T) {
	c := newTokenCache()
	tok, _ := generateToken()
	c.put(tok, "dev-1")
	c.invalidate("dev-1")
	if _, ok := c.get(tok); ok {
		t.Errorf("cache hit after invalidate")
	}
}
