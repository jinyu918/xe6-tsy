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
)

const shutdownTimeout = 10 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv, nil); err != nil {
		slog.Error("realtime-audio exited", "error", err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	getenv func(string) string,
	listenAndServe func(*http.Server) error,
) error {
	cfg, err := loadProcessConfig(getenv)
	if err != nil {
		return err
	}
	application, err := newControlPlaneRuntimeWithConfig(ctx, cfg)
	if err != nil {
		return err
	}
	server := newHTTPServer(cfg.Addr, application.Handler)
	if listenAndServe == nil {
		listenAndServe = func(s *http.Server) error { return s.ListenAndServe() }
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("realtime-audio control-plane listening", "addr", cfg.Addr)
		err := listenAndServe(server)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		shutdownErr := server.Shutdown(shutdownCtx)
		closeErr := application.Close(shutdownCtx)
		listenerErr := <-errCh
		return errors.Join(shutdownErr, closeErr, listenerErr)
	case err := <-errCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return errors.Join(err, application.Close(shutdownCtx))
	}
}
