package main

import "fmt"

// trayMode determines whether the tray runs a local forwarding runtime or
// acts as a UI-only client to an installed Windows service.
type trayMode int

const (
	trayModeLocal  trayMode = iota // service not installed — run local runtime
	trayModeClient                  // service installed — UI-only client
)

// selectTrayMode decides whether the tray should start a local forwarding
// runtime or act as a client to an installed service. When the service is
// installed in any state (running or stopped), the tray is a client to avoid
// racing over config, firewall rules, or listen ports.
func selectTrayMode(state serviceState) trayMode {
	if state == serviceNotInstalled {
		return trayModeLocal
	}
	return trayModeClient
}

// doEnableService implements the enable-service-mode flow: install the
// service with automatic startup, then start it. The install and start
// functions are injected for testability.
func doEnableService(installFn func(string) error, startFn func() error, executable string) error {
	if err := installFn(executable); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	if err := startFn(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	return nil
}

// doDisableService implements the disable-service-mode flow: stop the
// service, then delete it. Both functions are injected for testability.
// If either step fails, the error is returned and the caller must keep
// the tray in service-client mode rather than falling back to local.
func doDisableService(stopFn func() error, deleteFn func() error) error {
	if err := stopFn(); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	if err := deleteFn(); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	return nil
}

// trayConfig holds all callbacks and state the systray event loop needs.
// In local mode the service-related fields are zero/nil. In client mode
// OnQuit must not stop the service — only the tray process.
type trayConfig struct {
	Tooltip      string
	DashboardURL string
	OnOpen       func()
	OnQuit       func()

	Mode         trayMode
	ServiceState serviceState

	OnEnableService  func() error
	OnDisableService func() error
	OnStartService   func() error
	OnStopService    func() error
	QueryState       func() serviceState
}
