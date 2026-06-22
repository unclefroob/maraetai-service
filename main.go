// Command maraetai-service is a transparent reverse proxy in front of a Navidrome
// server. M0 forwards all traffic unchanged; later milestones tee play history
// and serve new Subsonic-shaped endpoints from the same binary.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/unclefroob/maraetai-service/internal/config"
	"github.com/unclefroob/maraetai-service/internal/proxy"
	"github.com/unclefroob/maraetai-service/internal/store"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Error("store", "err", err)
		os.Exit(1)
	}
	defer func() { _ = st.Close() }()

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           proxy.New(cfg.NavidromeURL, st, log),
		ReadHeaderTimeout: 10 * time.Second,
		// No write timeout: audio streams are long-lived.
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("listening", "addr", cfg.ListenAddr, "upstream", cfg.NavidromeURL.String(), "db", cfg.DBPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
		os.Exit(1)
	}
}
