package server

import (
	"net/http"
	"time"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"ok":         true,
		"version":    s.opts.Version,
		"uptime_sec": int(time.Since(s.started).Seconds()),
	})
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request, d *hub.Device) {
	writeJSON(w, 200, map[string]any{
		"ok":          true,
		"device_id":   d.ID,
		"account_id":  d.AccountID,
		"server_time": time.Now().UTC().Format(time.RFC3339),
	})
}
