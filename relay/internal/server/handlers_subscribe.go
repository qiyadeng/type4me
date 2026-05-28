package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

const heartbeatInterval = 25 * time.Second

func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request, d *hub.Device) {
	sw := newSSEWriter(w)
	_, _ = w.Write([]byte("retry: 5000\n\n"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	ch, unsub := s.opts.Hub.Subscribe(d.ID)
	defer unsub()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := sw.heartbeat(); err != nil {
				return
			}
		case msg, ok := <-ch:
			if !ok {
				return
			}
			payload := map[string]any{
				"text":               msg.Text,
				"request_id":         msg.RequestID,
				"from_device":        msg.FromDevice,
				"preserve_clipboard": msg.PreserveClipboard,
			}
			data, _ := json.Marshal(payload)
			if err := sw.frame(msg.ID, "inject", string(data)); err != nil {
				return
			}
		}
	}
}
