package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/qiyadeng/type4me/relay/internal/hub"
)

type Options struct {
	Hub         *hub.Hub
	AdminToken  string
	Version     string
	InviteCodes []string
	SessionKey  []byte
}

type Server struct {
	opts        Options
	started     time.Time
	signer      sessionSigner
	inviteCodes map[string]struct{}
	limiter     *rateLimiter
}

func New(opts Options) *Server {
	invites := map[string]struct{}{}
	for _, c := range opts.InviteCodes {
		c = strings.TrimSpace(c)
		if c != "" {
			invites[c] = struct{}{}
		}
	}
	return &Server{
		opts:        opts,
		started:     time.Now().UTC(),
		signer:      newSessionSigner(opts.SessionKey),
		inviteCodes: invites,
		limiter:     newRateLimiter(authRateLimit, authRateWindow),
	}
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

	mux.HandleFunc("/v1/auth/register", s.limiter.wrap(s.handleRegister))
	mux.HandleFunc("/v1/auth/login", s.limiter.wrap(s.handleLogin))
	mux.HandleFunc("/v1/devices", requireSession(s.signer,
		func(w http.ResponseWriter, r *http.Request, accountID string) {
			switch r.Method {
			case "POST":
				s.handlePostDevice(w, r, accountID)
			case "GET":
				s.handleGetDevices(w, r, accountID)
			default:
				w.WriteHeader(405)
			}
		}))
	return mux
}
