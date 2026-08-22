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

// doEnableService transfers port ownership from the local runtime to the
// service. The service is installed before local listeners are released. If
// service start fails, the service is removed and the local runtime restored;
// a cleanup failure leaves local listeners stopped to avoid a port collision.
func doEnableService(installFn func(string) error, stopLocalFn func(), startFn func() error, cleanupFn func() error, startLocalFn func() error, executable string) error {
	if err := installFn(executable); err != nil {
		return fmt.Errorf("install: %w", err)
	}

	stopLocalFn()
	if err := startFn(); err != nil {
		if cleanupErr := cleanupFn(); cleanupErr != nil {
			return fmt.Errorf("start: %v; cleanup failed: %w", err, cleanupErr)
		}
		if restoreErr := startLocalFn(); restoreErr != nil {
			return fmt.Errorf("start: %v; restore local runtime: %w", err, restoreErr)
		}
		return fmt.Errorf("start: %w", err)
	}
	return nil
}

// doDisableService removes the service. A running service must be stopped
// first; an already-stopped service can be deleted immediately. If either
// required step fails, the tray must remain in service-client mode.
func doDisableService(state serviceState, stopFn func() error, deleteFn func() error) error {
	if state == serviceRunning {
		if err := stopFn(); err != nil {
			return fmt.Errorf("stop: %w", err)
		}
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
