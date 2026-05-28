package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

func (s *Server) handleAdminCreateAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	acc, err := s.opts.Hub.AddAccount(req.Name)
	if errors.Is(err, hub.ErrAccountNameRequired) {
		writeJSON(w, 400, map[string]string{"error": "name_required"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "internal"})
		return
	}
	writeJSON(w, 201, map[string]any{
		"account_id": acc.ID,
		"name":       acc.Name,
		"created_at": acc.CreatedAt,
	})
}

func (s *Server) handleAdminListAccounts(w http.ResponseWriter, r *http.Request) {
	accs := s.opts.Hub.ListAccounts()
	out := make([]map[string]any, 0, len(accs))
	for _, a := range accs {
		out = append(out, map[string]any{
			"account_id": a.ID,
			"name":       a.Name,
			"created_at": a.CreatedAt,
		})
	}
	writeJSON(w, 200, map[string]any{"accounts": out})
}

func (s *Server) handleAdminCreateDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string   `json:"account_id"`
		Label     string   `json:"label"`
		Role      hub.Role `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	if req.AccountID == "" {
		writeJSON(w, 400, map[string]string{"error": "account_id_required"})
		return
	}
	dev, tok, err := s.opts.Hub.AddDevice(req.AccountID, req.Label, req.Role)
	if errors.Is(err, hub.ErrAccountNotFound) {
		writeJSON(w, 404, map[string]string{"error": "account_not_found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "internal"})
		return
	}
	writeJSON(w, 201, map[string]any{
		"device_id":    dev.ID,
		"device_token": tok,
		"account_id":   dev.AccountID,
		"label":        dev.Label,
		"role":         dev.Role,
		"created_at":   dev.CreatedAt,
	})
}

func (s *Server) handleAdminListDevices(w http.ResponseWriter, r *http.Request) {
	devs := s.opts.Hub.ListDevices()
	out := make([]map[string]any, 0, len(devs))
	for _, d := range devs {
		out = append(out, map[string]any{
			"id":         d.ID,
			"account_id": d.AccountID,
			"label":      d.Label,
			"role":       d.Role,
			"created_at": d.CreatedAt,
			"last_seen":  d.LastSeen,
		})
	}
	writeJSON(w, 200, map[string]any{"devices": out})
}

func (s *Server) handleAdminRotateDevice(w http.ResponseWriter, r *http.Request) {
	id := extractDeviceID(r.URL.Path, "/v1/admin/devices/", "/rotate")
	tok, err := s.opts.Hub.RotateDeviceToken(id)
	if errors.Is(err, hub.ErrDeviceNotFound) {
		writeJSON(w, 404, map[string]string{"error": "device_not_found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "internal"})
		return
	}
	writeJSON(w, 200, map[string]string{"device_token": tok})
}

func (s *Server) handleAdminDeleteDevice(w http.ResponseWriter, r *http.Request) {
	id := extractDeviceID(r.URL.Path, "/v1/admin/devices/", "")
	if err := s.opts.Hub.DeleteDevice(id); err != nil {
		if errors.Is(err, hub.ErrDeviceNotFound) {
			writeJSON(w, 404, map[string]string{"error": "device_not_found"})
			return
		}
		writeJSON(w, 500, map[string]string{"error": "internal"})
		return
	}
	w.WriteHeader(204)
}

func extractDeviceID(path, prefix, suffix string) string {
	p := strings.TrimPrefix(path, prefix)
	return strings.TrimSuffix(p, suffix)
}
