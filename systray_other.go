//go:build !windows

package main

import "log"

// runSystray is a no-op stub on non-Windows platforms. The real
// implementation (systray_windows.go) uses raw Win32 syscalls and only
// builds for GOOS=windows. This stub exists purely so the project can be
// built and tested on Linux/macOS during development.
func runSystray(cfg *trayConfig) error {
	log.Println("systray: not supported on this OS, skipping (dashboard still available over HTTP)")
	select {} // block forever, like the real message loop would
}
