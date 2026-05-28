package server

import (
	"net/http"
	"strings"
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
	mux.HandleFunc("/v1/dispatch", requireDevice(s.opts.Hub, s.handleDispatch))
	mux.HandleFunc("/v1/subscribe", requireDevice(s.opts.Hub, s.handleSubscribe))

	mux.HandleFunc("/v1/admin/accounts", requireAdmin(s.opts.AdminToken,
		func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "POST":
				s.handleAdminCreateAccount(w, r)
			case "GET":
				s.handleAdminListAccounts(w, r)
			default:
				w.WriteHeader(405)
			}
		}))

	mux.HandleFunc("/v1/admin/devices", requireAdmin(s.opts.AdminToken,
		func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "POST":
				s.handleAdminCreateDevice(w, r)
			case "GET":
				s.handleAdminListDevices(w, r)
			default:
				w.WriteHeader(405)
			}
		}))

	mux.HandleFunc("/v1/admin/devices/", requireAdmin(s.opts.AdminToken,
		func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/rotate"):
				s.handleAdminRotateDevice(w, r)
			case r.Method == "DELETE":
				s.handleAdminDeleteDevice(w, r)
			default:
				w.WriteHeader(405)
			}
		}))
	return mux
}
