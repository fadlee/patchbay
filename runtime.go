package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"
)

// runtimeApp owns the config store, forwarding manager, HTTP dashboard, and
// their lifecycle. Both local tray mode and Windows service mode use it.
type runtimeApp struct {
	store                 *ConfigStore
	manager               *Manager
	app                   *App
	server                *http.Server
	listener              net.Listener
	dashboardRetryTimeout time.Duration
}

// newRuntime constructs a runtime from an already-loaded config store and
// manager. The HTTP dashboard address is derived from the config's AdminPort.
func newRuntime(store *ConfigStore, manager *Manager) *runtimeApp {
	cfg := store.Snapshot()
	addr := "127.0.0.1:" + strconv.Itoa(cfg.AdminPort)
	return &runtimeApp{
		store:                 store,
		manager:               manager,
		app:                   NewApp(store, manager),
		server:                &http.Server{Addr: addr, Handler: nil},
		dashboardRetryTimeout: 5 * time.Second,
	}
}

// dashboardURL returns the URL a browser or tray should open.
func (rt *runtimeApp) dashboardURL() string {
	return "http://" + rt.server.Addr + "/"
}

// start reserves the HTTP dashboard port before starting enabled forwarding
// rules. This prevents a failed dashboard bind from leaving a partial local
// forwarding runtime alive during a service-to-tray handoff.
func (rt *runtimeApp) start(ctx context.Context) error {
	ln, err := rt.listenDashboard()
	if err != nil {
		return err
	}
	rt.listener = ln
	rt.server.Handler = rt.app.Routes()

	cfg := rt.store.Snapshot()
	for _, r := range cfg.Rules {
		if !r.Enabled {
			continue
		}
		if err := rt.manager.Start(r); err != nil {
			log.Printf("failed to start rule %q: %v", r.Name, err)
		}
	}

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

// listenDashboard waits briefly only for an address-in-use error. Windows
// can report SCM stopped before its process has released the HTTP listener.
func (rt *runtimeApp) listenDashboard() (net.Listener, error) {
	deadline := time.Now().Add(rt.dashboardRetryTimeout)
	for {
		ln, err := net.Listen("tcp", rt.server.Addr)
		if err == nil {
			return ln, nil
		}
		if !errors.Is(err, syscall.EADDRINUSE) || !time.Now().Before(deadline) {
			return nil, fmt.Errorf("dashboard listen: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
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
