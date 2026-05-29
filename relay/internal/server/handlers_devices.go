package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

func (s *Server) handlePostDevice(w http.ResponseWriter, r *http.Request, accountID string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	var req struct {
		Label string   `json:"label"`
		Role  hub.Role `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	dev, tok, err := s.opts.Hub.AddDevice(accountID, req.Label, req.Role)
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
		"label":        dev.Label,
		"role":         dev.Role,
		"created_at":   dev.CreatedAt,
	})
}

func (s *Server) handleGetDevices(w http.ResponseWriter, r *http.Request, accountID string) {
	devs := s.opts.Hub.ListDevicesByAccount(accountID)
	out := make([]map[string]any, 0, len(devs))
	for _, d := range devs {
		out = append(out, map[string]any{
			"id":        d.ID,
			"label":     d.Label,
			"role":      d.Role,
			"last_seen": d.LastSeen,
			"online":    s.opts.Hub.IsOnline(d.ID),
		})
	}
	writeJSON(w, 200, map[string]any{"devices": out})
}
