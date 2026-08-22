package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"strconv"
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
	svcState, _ := queryService()
	mode := selectTrayMode(svcState)

	var rt *runtimeApp
	if mode == trayModeLocal {
		manager := NewManager()
		rt = newRuntime(store, manager)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if err := rt.start(ctx); err != nil {
			return fmt.Errorf("failed to start runtime: %w", err)
		}

		// Allow Ctrl+C during development (the real Windows build exits via
		// the tray Quit item, but this makes non-Windows/dev runs
		// interruptible too).
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigCh
			rt.stop()
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

	if mode == trayModeLocal {
		trayCfg.OnQuit = func() { rt.stop(); os.Exit(0) }
		trayCfg.OnEnableService = func() error {
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve executable: %w", err)
			}
			return doEnableService(installService, startService, exe)
		}
	} else {
		// Client mode: Quit exits only the tray, not the service.
		trayCfg.OnQuit = func() { os.Exit(0) }
		trayCfg.OnStartService = startService
		trayCfg.OnStopService = stopService
		trayCfg.OnDisableService = func() error {
			return doDisableService(stopService, deleteService)
		}
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
