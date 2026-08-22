package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
)

// runtimeApp owns the config store, forwarding manager, HTTP dashboard, and
// their lifecycle. Both local tray mode and Windows service mode use it.
type runtimeApp struct {
	store    *ConfigStore
	manager  *Manager
	app      *App
	server   *http.Server
	listener net.Listener
}

// newRuntime constructs a runtime from an already-loaded config store and
// manager. The HTTP dashboard address is derived from the config's AdminPort.
func newRuntime(store *ConfigStore, manager *Manager) *runtimeApp {
	cfg := store.Snapshot()
	addr := "127.0.0.1:" + strconv.Itoa(cfg.AdminPort)
	return &runtimeApp{
		store:   store,
		manager: manager,
		app:     NewApp(store, manager),
		server:  &http.Server{Addr: addr, Handler: nil},
	}
}

// dashboardURL returns the URL a browser or tray should open.
func (rt *runtimeApp) dashboardURL() string {
	return "http://" + rt.server.Addr + "/"
}

// start begins enabled forwarding rules and serves the HTTP dashboard. It
// returns once the dashboard listener is bound; serving continues in the
// background until stop is called or the context is cancelled.
func (rt *runtimeApp) start(ctx context.Context) error {
	cfg := rt.store.Snapshot()
	for _, r := range cfg.Rules {
		if !r.Enabled {
			continue
		}
		if err := rt.manager.Start(r); err != nil {
			log.Printf("failed to start rule %q: %v", r.Name, err)
		}
	}

	ln, err := net.Listen("tcp", rt.server.Addr)
	if err != nil {
		return fmt.Errorf("dashboard listen: %w", err)
	}
	rt.listener = ln
	rt.server.Handler = rt.app.Routes()

	go func() {
		log.Printf("dashboard listening on %s", rt.dashboardURL())
		if err := rt.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("dashboard server failed: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()
		rt.stop()
	}()

	return nil
}

// stop closes the HTTP dashboard listener and all forwarding rules. It is
// safe to call multiple times.
func (rt *runtimeApp) stop() {
	if rt.server != nil {
		if err := rt.server.Close(); err != nil {
			log.Printf("dashboard server close: %v", err)
		}
	}
	rt.manager.StopAll()
}
