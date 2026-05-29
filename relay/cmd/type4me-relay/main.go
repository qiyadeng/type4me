package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/qiyadeng/type4me/relay/internal/hub"
	"github.com/qiyadeng/type4me/relay/internal/server"
)

var version = "dev"

func main() {
	var stateDir string
	flag.StringVar(&stateDir, "state-dir", "", "directory for state.json (overrides $TYPE4ME_RELAY_STATE_DIR)")
	flag.Parse()

	if flag.Arg(0) != "serve" {
		fmt.Fprintln(os.Stderr, "usage: type4me-relay [--state-dir DIR] serve")
		os.Exit(2)
	}

	admin := os.Getenv("TYPE4ME_RELAY_ADMIN_TOKEN")
	if admin == "" {
		log.Fatal("TYPE4ME_RELAY_ADMIN_TOKEN env var required")
	}
	bind := os.Getenv("TYPE4ME_RELAY_BIND")
	if bind == "" {
		bind = "127.0.0.1:8443"
	}
	if stateDir == "" {
		stateDir = os.Getenv("TYPE4ME_RELAY_STATE_DIR")
		if stateDir == "" {
			stateDir = "/var/lib/type4me-relay"
		}
	}

	h, err := hub.New(filepath.Join(stateDir, "state.json"))
	if err != nil {
		log.Fatalf("hub init: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go h.RunScrubber(ctx)

	inviteCodes := splitAndTrim(os.Getenv("TYPE4ME_RELAY_INVITE_CODES"))
	sessionKey := []byte(os.Getenv("TYPE4ME_RELAY_SESSION_KEY"))
	if len(sessionKey) == 0 {
		sessionKey = make([]byte, 32)
		if _, err := rand.Read(sessionKey); err != nil {
			log.Fatalf("generate session key: %v", err)
		}
		log.Println("WARNING: TYPE4ME_RELAY_SESSION_KEY unset; using a random key. All sessions invalidate on restart.")
	}

	srv := server.New(server.Options{
		Hub:         h,
		AdminToken:  admin,
		Version:     version,
		InviteCodes: inviteCodes,
		SessionKey:  sessionKey,
	})

	httpSrv := &http.Server{
		Addr:              bind,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("type4me-relay %s listening on %s (state: %s)", version, bind, stateDir)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(sctx)
}

// splitAndTrim splits a comma-separated string, trims whitespace, and drops empty items.
func splitAndTrim(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
