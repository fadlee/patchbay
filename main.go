package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
)

// dispatchMode selects between service and tray entry points based on the
// first command-line argument. It is extracted from main for testability.
func dispatchMode(args []string, serviceFn, trayFn func() error) error {
	if len(args) > 1 && args[1] == "service" {
		return serviceFn()
	}
	return trayFn()
}

func main() {
	if err := dispatchMode(os.Args, runService, runTray); err != nil {
		log.Fatalf("%v", err)
	}
}

// runTray starts either a local forwarding runtime (when no service is
// installed) or a UI-only client (when the service is installed), then
// enters the systray event loop.
func runTray() error {
	store, err := NewConfigStore("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	cfg := store.Snapshot()
	dashboardURL := "http://127.0.0.1:" + strconv.Itoa(cfg.AdminPort) + "/"

	// Query SCM to decide whether we run a local runtime or act as client.
	svcState, err := queryService()
	if err != nil {
		return fmt.Errorf("query service state: %w", err)
	}
	mode := selectTrayMode(svcState)

	var rt *runtimeApp
	startLocalRuntime := func() error {
		local := newRuntime(store, NewManager())
		if err := local.start(context.Background()); err != nil {
			return fmt.Errorf("start local runtime: %w", err)
		}
		rt = local
		return nil
	}
	stopLocalRuntime := func() {
		if rt != nil {
			rt.stop()
			rt = nil
		}
	}

	if mode == trayModeLocal {
		if err := startLocalRuntime(); err != nil {
			return err
		}

		// Allow Ctrl+C during development (the real Windows build exits via
		// the tray Quit item, but this makes non-Windows/dev runs
		// interruptible too).
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigCh
			stopLocalRuntime()
			os.Exit(0)
		}()
	} else {
		log.Printf("service mode active (state=%s) — tray running as client", svcState)
	}

	// Win32 requires the message loop to run on the thread that created the
	// window, so we lock the main goroutine to its OS thread before handing
	// control to the tray's blocking event loop.
	runtime.LockOSThread()

	trayCfg := &trayConfig{
		Tooltip:      "patchbay — port forwarding",
		DashboardURL: dashboardURL,
		Mode:         mode,
		ServiceState: svcState,
		OnOpen:       func() { openBrowser(dashboardURL) },
	}
	trayCfg.OnQuit = func() { stopLocalRuntime(); os.Exit(0) }

	var setLocalMode func()
	var setClientMode func(serviceState)

	setLocalMode = func() {
		trayCfg.Mode = trayModeLocal
		trayCfg.ServiceState = serviceNotInstalled
		trayCfg.OnStartService = nil
		trayCfg.OnStopService = nil
		trayCfg.OnDisableService = nil
		trayCfg.OnEnableService = func() error {
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve executable: %w", err)
			}
			if err := doEnableService(installService, stopLocalRuntime, startService, deleteService, startLocalRuntime, exe); err != nil {
				return err
			}
			setClientMode(serviceRunning)
			return nil
		}
	}

	setClientMode = func(state serviceState) {
		trayCfg.Mode = trayModeClient
		trayCfg.ServiceState = state
		trayCfg.OnEnableService = nil
		trayCfg.OnStartService = func() error {
			if err := startService(); err != nil {
				return err
			}
			trayCfg.ServiceState = serviceRunning
			return nil
		}
		trayCfg.OnStopService = func() error {
			if err := stopService(); err != nil {
				return err
			}
			trayCfg.ServiceState = serviceStopped
			return nil
		}
		trayCfg.OnDisableService = func() error {
			if err := doDisableService(trayCfg.ServiceState, stopService, deleteService); err != nil {
				return err
			}
			if err := startLocalRuntime(); err != nil {
				return err
			}
			setLocalMode()
			return nil
		}
	}

	if mode == trayModeLocal {
		setLocalMode()
	} else {
		setClientMode(svcState)
	}

	return runSystray(trayCfg)
}

// openBrowser launches the user's default browser to the dashboard URL.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("failed to open browser: %v", err)
		fmt.Println("open this URL manually:", url)
	}
}
