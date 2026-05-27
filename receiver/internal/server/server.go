package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/qiyadeng/type4me/receiver/internal/inject"
)

const (
	MaxBodyBytes = 16 * 1024            // 16 KB headroom; we enforce 8KB text below
	MaxTextBytes = 8 * 1024
	MutexHold    = 500 * time.Millisecond // wait at most 500ms for injector mutex
)

type Options struct {
	Token    string
	Injector inject.Injector
	Name     string
	Platform string
	Version  string
}

// Server is the HTTP receiver server.
// mu is a 1-slot channel used as a semaphore to serialize inject calls.
type Server struct {
	opts Options
	mu   chan struct{}
}

func New(opts Options) *Server {
	return &Server{opts: opts, mu: make(chan struct{}, 1)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", s.handlePing)
	mux.HandleFunc("/info", s.requireAuth(s.handleInfo))
	mux.HandleFunc("/inject", s.requireAuth(s.handleInject))
	return mux
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"ok":       true,
		"name":     s.opts.Name,
		"platform": s.opts.Platform,
		"version":  s.opts.Version,
	})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"name":     s.opts.Name,
		"platform": s.opts.Platform,
	})
}

type injectRequest struct {
	Text              string `json:"text"`
	RequestID         string `json:"request_id"`
	PreserveClipboard *bool  `json:"preserve_clipboard"`
}

func (s *Server) handleInject(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	var req injectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	if len(req.Text) > MaxTextBytes {
		writeJSON(w, 413, map[string]any{"error": "text_too_large", "max_bytes": MaxTextBytes})
		return
	}

	// Acquire inject mutex (best-effort, 500ms ceiling).
	select {
	case s.mu <- struct{}{}:
		defer func() { <-s.mu }()
	case <-time.After(MutexHold):
		writeJSON(w, 423, map[string]string{"error": "concurrent_inject"})
		return
	}

	out, err := s.opts.Injector.Inject(req.Text)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "platform_error", "detail": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok":         out.Pasted,
		"request_id": req.RequestID,
		"outcome":    out,
	})
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(h, prefix) {
			writeJSON(w, 401, map[string]string{"error": "invalid_token"})
			return
		}
		got := h[len(prefix):]
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.opts.Token)) != 1 {
			writeJSON(w, 401, map[string]string{"error": "invalid_token"})
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil && !errors.Is(err, http.ErrBodyNotAllowed) {
		// best effort; nothing else to do
	}
}
