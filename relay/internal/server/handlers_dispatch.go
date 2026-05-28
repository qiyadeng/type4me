package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

const maxTextBytes = 8 * 1024

type dispatchRequest struct {
	TargetDeviceID    string `json:"target_device_id"`
	Text              string `json:"text"`
	RequestID         string `json:"request_id"`
	PreserveClipboard bool   `json:"preserve_clipboard"`
}

func (s *Server) handleDispatch(w http.ResponseWriter, r *http.Request, sender *hub.Device) {
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxTextBytes)*2)
	var req dispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	if len(req.Text) > maxTextBytes {
		writeJSON(w, 413, map[string]any{"error": "text_too_large", "max_bytes": maxTextBytes})
		return
	}
	tok := extractBearer(r)
	msg, err := s.opts.Hub.Dispatch(tok, req.TargetDeviceID, req.Text, req.RequestID, req.PreserveClipboard)
	switch {
	case errors.Is(err, hub.ErrInvalidToken):
		writeJSON(w, 401, map[string]string{"error": "invalid_sender_token"})
	case errors.Is(err, hub.ErrDeviceNotFound):
		writeJSON(w, 404, map[string]string{"error": "target_not_found"})
	case errors.Is(err, hub.ErrCrossAccount):
		writeJSON(w, 403, map[string]string{"error": "cross_account"})
	case errors.Is(err, hub.ErrReceiverOffline):
		writeJSON(w, 503, map[string]string{"error": "receiver_offline"})
	case errors.Is(err, hub.ErrBackpressure):
		writeJSON(w, 503, map[string]string{"error": "receiver_backpressure"})
	case err != nil:
		writeJSON(w, 500, map[string]string{"error": "internal"})
	default:
		writeJSON(w, 202, map[string]any{"accepted": true, "message_id": msg.ID})
	}
	_ = sender
}
