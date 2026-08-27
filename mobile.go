package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

var (
	mobileMu      sync.Mutex
	mobileRuntime *runtimeApp
	mobileCancel  context.CancelFunc
)

// StartMobile starts the Patchbay runtime for mobile/Android environments.
// dataDir specifies the writable root folder for config and logs (e.g. Android's context.filesDir).
// adminHost is typically "127.0.0.1" (or "0.0.0.0" if remote dashboard access is desired).
// adminPort defaults to 8787 if <= 0.
func StartMobile(dataDir string, adminHost string, adminPort int) error {
	mobileMu.Lock()
	defer mobileMu.Unlock()

	if mobileRuntime != nil {
		return fmt.Errorf("patchbay is already running")
	}

	if dataDir == "" {
		dataDir = "."
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	configPath := filepath.Join(dataDir, configFileName)
	logDir := filepath.Join(dataDir, "logs")

	store, err := NewConfigStore(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	cfg := store.Snapshot()
	if adminPort > 0 && cfg.AdminPort != adminPort {
		cfg.AdminPort = adminPort
		store.mu.Lock()
		store.cfg.AdminPort = adminPort
		_ = store.saveLocked()
		store.mu.Unlock()
	}

	logger, err := NewTrafficLogger(logDir, 1000)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}

	manager := NewManager()
	hub := NewSSEHub()
	if manager != nil && logger != nil {
		manager.SetLogger(logger)
	}
	if logger != nil && hub != nil {
		logger.SetOnRecord(func(entry LogEntry) {
			hub.Broadcast("log", entry)
		})
	}
	if logger != nil {
		logger.SetEnabled(cfg.IsLoggingEnabled())
	}

	if adminHost == "" {
		adminHost = "127.0.0.1"
	}
	addr := net.JoinHostPort(adminHost, strconv.Itoa(cfg.AdminPort))
	app := NewApp(store, manager, logger, hub)

	rt := &runtimeApp{
		store:   store,
		manager: manager,
		logger:  logger,
		hub:     hub,
		app:     app,
		server:  &http.Server{Addr: addr, Handler: app.Routes()},
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := rt.start(ctx); err != nil {
		cancel()
		return fmt.Errorf("start runtime: %w", err)
	}

	mobileRuntime = rt
	mobileCancel = cancel
	return nil
}

// StopMobile stops the running mobile runtime and all active proxies.
func StopMobile() {
	mobileMu.Lock()
	defer mobileMu.Unlock()

	if mobileCancel != nil {
		mobileCancel()
		mobileCancel = nil
	}
	if mobileRuntime != nil {
		mobileRuntime.stop()
		mobileRuntime = nil
	}
}

// IsMobileRunning returns whether the Patchbay mobile runtime is currently active.
func IsMobileRunning() bool {
	mobileMu.Lock()
	defer mobileMu.Unlock()
	return mobileRuntime != nil
}

// GetMobileDashboardURL returns the dashboard HTTP URL if running, or empty string.
func GetMobileDashboardURL() string {
	mobileMu.Lock()
	defer mobileMu.Unlock()
	if mobileRuntime == nil || mobileRuntime.server == nil {
		return ""
	}
	return "http://" + mobileRuntime.server.Addr + "/"
}
