package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/qiyadeng/type4me/receiver/internal/config"
	"github.com/qiyadeng/type4me/receiver/internal/inject"
	"github.com/qiyadeng/type4me/receiver/internal/relay"
	"github.com/qiyadeng/type4me/receiver/internal/server"
)

var version = "dev"

func main() {
	var cfgPath string
	flag.StringVar(&cfgPath, "config", defaultConfigPath(), "path to config.json")
	flag.Parse()

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0700); err != nil {
		log.Fatalf("mkdir config dir: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("config load: %v", err)
	}
	if err := cfg.Save(cfgPath); err != nil {
		log.Printf("config save (will retry on changes): %v", err)
	}

	inj := inject.NewPlatform()
	if err := inj.Ping(); err != nil {
		log.Fatalf("inject platform unavailable: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch cfg.Mode {
	case config.ModeListener:
		runListener(ctx, cfg, inj)
	case config.ModeRelaySubscriber:
		runRelaySubscriber(ctx, cfg, inj)
	default:
		log.Fatalf("unknown mode: %s", cfg.Mode)
	}
}

func runListener(ctx context.Context, cfg *config.Config, inj inject.Injector) {
	s := server.New(server.Options{
		Token:    cfg.Token,
		Injector: inj,
		Name:     cfg.Name,
		Platform: runtime.GOOS,
		Version:  version,
	})
	addr := fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.Port)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 3 * time.Second,
	}
	printListenerPairing(cfg, addr)
	go func() {
		log.Printf("listening on %s", addr)
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

func runRelaySubscriber(ctx context.Context, cfg *config.Config, inj inject.Injector) {
	sub := &relay.Subscriber{
		RelayURL:    cfg.RelayURL,
		DeviceToken: cfg.DeviceToken,
		Injector:    inj,
		HTTPClient:  &http.Client{Timeout: 0}, // SSE 长连,不设总超时
	}
	log.Printf("subscribing to %s as %s (platform=%s)",
		cfg.RelayURL, cfg.DeviceID, runtime.GOOS)
	if err := sub.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("subscriber: %v", err)
	}
	log.Println("subscriber exited")
}

func defaultConfigPath() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support",
			"type4me-receiver", "config.json")
	case "windows":
		appdata := os.Getenv("APPDATA")
		return filepath.Join(appdata, "type4me-receiver", "config.json")
	default:
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "type4me-receiver", "config.json")
	}
}

// printListenerPairing prints a developer-friendly summary to stdout. Once we
// add tray UI (S4), it will move into a pairing window.
func printListenerPairing(cfg *config.Config, addr string) {
	urlHost := cfg.BindAddr
	if urlHost == "0.0.0.0" || urlHost == "::" {
		urlHost = "127.0.0.1"
	}
	fmt.Println()
	fmt.Println("================ type4me-receiver pairing (LISTENER MODE) ================")
	fmt.Printf("  Name:    %s\n", cfg.Name)
	fmt.Printf("  Addr:    %s\n", addr)
	fmt.Printf("  Token:   %s\n", cfg.Token)
	fmt.Printf("  URL:     type4me://pair?host=%s&port=%d&token=%s&name=%s&platform=%s\n",
		urlHost, cfg.Port, cfg.Token, url.QueryEscape(cfg.Name), runtime.GOOS)
	fmt.Println("==========================================================================")
	fmt.Println()
}
