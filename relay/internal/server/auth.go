package server

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

const bearerPrefix = "Bearer "

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, bearerPrefix) {
		return ""
	}
	return h[len(bearerPrefix):]
}

func requireAdmin(adminToken string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := extractBearer(r)
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(adminToken)) != 1 {
			writeJSON(w, 401, map[string]string{"error": "invalid_admin_token"})
			return
		}
		next(w, r)
	}
}

type deviceHandler func(w http.ResponseWriter, r *http.Request, d *hub.Device)

func requireDevice(h *hub.Hub, next deviceHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := extractBearer(r)
		if tok == "" {
			writeJSON(w, 401, map[string]string{"error": "missing_token"})
			return
		}
		dev, err := h.ResolveDeviceByToken(tok)
		if err != nil {
			writeJSON(w, 401, map[string]string{"error": "invalid_sender_token"})
			return
		}
		next(w, r, dev)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type sessionHandler func(w http.ResponseWriter, r *http.Request, accountID string)

func requireSession(signer sessionSigner, next sessionHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accountID, err := signer.verify(extractBearer(r), time.Now().UTC())
		if err != nil {
			writeJSON(w, 401, map[string]string{"error": "invalid_session"})
			return
		}
		next(w, r, accountID)
	}
}
