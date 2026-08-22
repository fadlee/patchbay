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
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "service" {
		if err := runService(); err != nil {
			log.Fatalf("service error: %v", err)
		}
		return
	}

	store, err := NewConfigStore("")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	manager := NewManager()
	rt := newRuntime(store, manager)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rt.start(ctx); err != nil {
		log.Fatalf("failed to start runtime: %v", err)
	}

	// Allow Ctrl+C during development (the real Windows build exits via the
	// tray Quit item, but this makes non-Windows/dev runs interruptible too).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		rt.stop()
		os.Exit(0)
	}()

	// Win32 requires the message loop to run on the thread that created the
	// window, so we lock the main goroutine to its OS thread before handing
	// control to the tray's blocking event loop.
	runtime.LockOSThread()

	err = runSystray(
		"patchbay — port forwarding",
		func() { openBrowser(rt.dashboardURL()) },
		func() { rt.stop(); os.Exit(0) },
	)
	if err != nil {
		log.Printf("systray error: %v", err)
	}
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
