package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

const maxAuthBodyBytes = 4096

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		InviteCode string `json:"invite_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	if len(s.inviteCodes) == 0 {
		writeJSON(w, 403, map[string]string{"error": "registration_disabled"})
		return
	}
	if _, ok := s.inviteCodes[req.InviteCode]; !ok {
		writeJSON(w, 403, map[string]string{"error": "invalid_invite"})
		return
	}
	acc, err := s.opts.Hub.RegisterUser(req.Username, req.Password)
	switch {
	case errors.Is(err, hub.ErrUsernameInvalid):
		writeJSON(w, 400, map[string]string{"error": "username_invalid"})
		return
	case errors.Is(err, hub.ErrPasswordTooShort):
		writeJSON(w, 400, map[string]string{"error": "password_too_short"})
		return
	case errors.Is(err, hub.ErrPasswordTooLong):
		writeJSON(w, 400, map[string]string{"error": "password_too_long"})
		return
	case errors.Is(err, hub.ErrUsernameTaken):
		writeJSON(w, 409, map[string]string{"error": "username_taken"})
		return
	case err != nil:
		writeJSON(w, 500, map[string]string{"error": "internal"})
		return
	}
	s.writeSession(w, 201, acc)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	acc, err := s.opts.Hub.Authenticate(req.Username, req.Password)
	if errors.Is(err, hub.ErrInvalidCredentials) {
		writeJSON(w, 401, map[string]string{"error": "invalid_credentials"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "internal"})
		return
	}
	s.writeSession(w, 200, acc)
}

func (s *Server) writeSession(w http.ResponseWriter, status int, acc *hub.Account) {
	now := time.Now().UTC()
	writeJSON(w, status, map[string]any{
		"session_token": s.signer.sign(acc.ID, now),
		"account_id":    acc.ID,
		"username":      acc.Username,
		"expires_at":    now.Add(sessionTTL),
	})
}
