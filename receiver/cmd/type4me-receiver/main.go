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
	"runtime"
	"syscall"
	"time"

	"github.com/qiyadeng/type4me/receiver/internal/config"
	"github.com/qiyadeng/type4me/receiver/internal/inject"
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

	printPairingInfo(cfg, addr)

	go func() {
		log.Printf("listening on %s", addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Println("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
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

// printPairingInfo prints a developer-friendly summary to stdout. Once we add
// tray UI (S4), it will move into a pairing window.
func printPairingInfo(cfg *config.Config, addr string) {
	fmt.Println()
	fmt.Println("================ type4me-receiver pairing ================")
	fmt.Printf("  Name:    %s\n", cfg.Name)
	fmt.Printf("  Addr:    %s\n", addr)
	fmt.Printf("  Token:   %s\n", cfg.Token)
	fmt.Printf("  URL:     type4me://pair?host=%s&port=%d&token=%s&name=%s&platform=%s\n",
		cfg.BindAddr, cfg.Port, cfg.Token, cfg.Name, runtime.GOOS)
	fmt.Println("==========================================================")
	fmt.Println()
}
