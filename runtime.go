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
	logger                *TrafficLogger
	hub                   *SSEHub
	app                   *App
	server                *http.Server
	listener              net.Listener
	dashboardRetryTimeout time.Duration
}

// newRuntime constructs a runtime from an already-loaded config store and
// manager. The HTTP dashboard address is derived from the config's AdminPort.
func newRuntime(store *ConfigStore, manager *Manager) *runtimeApp {
	logger, _ := NewTrafficLogger("", 1000)
	hub := NewSSEHub()
	if manager != nil && logger != nil {
		manager.SetLogger(logger)
	}
	if logger != nil && hub != nil {
		logger.SetOnRecord(func(entry LogEntry) {
			hub.Broadcast("log", entry)
		})
	}

	cfg := store.Snapshot()
	addr := "127.0.0.1:" + strconv.Itoa(cfg.AdminPort)
	app := NewApp(store, manager, logger, hub)
	return &runtimeApp{
		store:                 store,
		manager:               manager,
		logger:                logger,
		hub:                   hub,
		app:                   app,
		server:                &http.Server{Addr: addr, Handler: app.Routes()},
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

	// Periodically broadcast live stats via SSE
	if rt.hub != nil {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					rt.hub.Broadcast("stats", map[string]any{
						"rules": rt.app.ruleViews(),
					})
				}
			}
		}()
	}

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
	if rt.hub != nil {
		rt.hub.Close()
	}
	if rt.logger != nil {
		_ = rt.logger.Close()
	}
	rt.manager.StopAll()
}
