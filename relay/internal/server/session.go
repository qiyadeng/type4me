package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const sessionTTL = 24 * time.Hour

var errInvalidSession = errors.New("invalid session")

type sessionSigner struct {
	key []byte
}

func newSessionSigner(key []byte) sessionSigner {
	return sessionSigner{key: key}
}

type sessionClaims struct {
	AID string `json:"aid"`
	Exp int64  `json:"exp"`
}

func (s sessionSigner) mac(payload string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// sign returns base64url(claims) + "." + base64url(HMAC).
func (s sessionSigner) sign(accountID string, now time.Time) string {
	claims := sessionClaims{AID: accountID, Exp: now.Add(sessionTTL).Unix()}
	raw, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + s.mac(payload)
}

// verify checks the signature and expiry, returning the accountID.
func (s sessionSigner) verify(token string, now time.Time) (string, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return "", errInvalidSession
	}
	if !hmac.Equal([]byte(parts[1]), []byte(s.mac(parts[0]))) {
		return "", errInvalidSession
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", errInvalidSession
	}
	var c sessionClaims
	if err := json.Unmarshal(raw, &c); err != nil {
		return "", errInvalidSession
	}
	if now.Unix() >= c.Exp {
		return "", errInvalidSession
	}
	return c.AID, nil
}
