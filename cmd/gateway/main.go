// Command gateway is the entry point for the enhanced relay gateway.
//
// Phase -1 scaffold: it wires the deterministic clock/ID generators, builds the
// enabled plugins from the registry, constructs the hook dispatcher, event bus
// and pipeline, and serves a health endpoint. Request transport and provider
// relay are added in later phases.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lzzen/enhanced-relay/internal/clock"
	"github.com/lzzen/enhanced-relay/internal/event"
	"github.com/lzzen/enhanced-relay/internal/hook"
	"github.com/lzzen/enhanced-relay/internal/idgen"
	"github.com/lzzen/enhanced-relay/internal/plugin"
	"github.com/lzzen/enhanced-relay/internal/plugin/builtin"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("gateway: %v", err)
	}
}

func run() error {
	clk := clock.New()
	ids := idgen.New()
	_ = ids // used by the pipeline once request transport lands

	// Compile-time plugin registry. Real builds populate this via init() and
	// select enabled plugins from the config snapshot.
	reg := plugin.NewRegistry()
	reg.Register(builtin.StampName, builtin.NewStamp)

	enabled := []string{builtin.StampName}
	plugins, err := reg.Build(enabled)
	if err != nil {
		return err
	}
	for _, p := range plugins {
		if err := p.Init(plugin.InitContext{}); err != nil {
			return err
		}
	}

	dispatcher := hook.NewDispatcher(plugin.HookRegistrations(plugins), nil)
	_ = dispatcher // consumed by the pipeline once request transport lands

	bus := event.New(1024)
	bus.Start()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	addr := os.Getenv("GATEWAY_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("gateway: listening on %s (plugins: %v)", addr, reg.Names())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Printf("gateway: shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	bus.Drain(shutdownCtx)

	_ = clk
	return nil
}
