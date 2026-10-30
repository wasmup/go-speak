package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

func run() error {
	cfg, err := ParseFlags()
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = exec.LookPath("aplay"); err != nil {
		return err
	}
	app, err := NewApp(cfg)
	if err != nil {
		return err
	}
	srv := newServer(cfg, app)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errs := make(chan error, 1)
	go func() { log.Printf("Listening on http://%s", cfg.Addr); errs <- srv.ListenAndServe() }()
	select {
	case err = <-errs:
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Cancel playback immediately; allow active HTTP requests and native cleanup
	// to finish within the shutdown deadline.
	app.mu.Lock()
	app.closed = true
	app.stopLocked()
	app.mu.Unlock()
	cleaned := make(chan struct{})
	go func() { app.Close(); close(cleaned) }()
	shutdownErr := srv.Shutdown(shutdownCtx)
	select {
	case <-cleaned:
	case <-shutdownCtx.Done():
		log.Print("shutdown timed out waiting for native synthesis")
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return shutdownErr
}

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
