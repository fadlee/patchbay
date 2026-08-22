package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
)

func main() {
	store, err := NewConfigStore("")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	cfg := store.Snapshot()

	manager := NewManager()
	for _, r := range cfg.Rules {
		if !r.Enabled {
			continue
		}
		if err := manager.Start(r); err != nil {
			log.Printf("failed to start rule %q: %v", r.Name, err)
		}
	}

	app := NewApp(store, manager)
	addr := "127.0.0.1:" + strconv.Itoa(cfg.AdminPort)
	dashboardURL := "http://" + addr + "/"

	go func() {
		log.Printf("dashboard listening on %s", dashboardURL)
		if err := http.ListenAndServe(addr, app.Routes()); err != nil {
			log.Fatalf("dashboard server failed: %v", err)
		}
	}()

	// Allow Ctrl+C during development (the real Windows build exits via the
	// tray Quit item, but this makes non-Windows/dev runs interruptible too).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		shutdown(manager)
	}()

	// Win32 requires the message loop to run on the thread that created the
	// window, so we lock the main goroutine to its OS thread before handing
	// control to the tray's blocking event loop.
	runtime.LockOSThread()

	err = runSystray(
		"patchbay — port forwarding",
		func() { openBrowser(dashboardURL) },
		func() { shutdown(manager) },
	)
	if err != nil {
		log.Printf("systray error: %v", err)
	}
}

func shutdown(manager *Manager) {
	manager.StopAll()
	os.Exit(0)
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
