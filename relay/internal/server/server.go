package server

import (
	"net/http"
	"time"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

type Options struct {
	Hub        *hub.Hub
	AdminToken string
	Version    string
}

type Server struct {
	opts    Options
	started time.Time
}

func New(opts Options) *Server {
	return &Server{opts: opts, started: time.Now().UTC()}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/v1/ping", requireDevice(s.opts.Hub, s.handlePing))
	return mux
}
