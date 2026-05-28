package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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

	srv := server.New(server.Options{
		Hub:        h,
		AdminToken: admin,
		Version:    version,
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
