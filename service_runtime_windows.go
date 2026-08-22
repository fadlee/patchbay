//go:build windows

package main

// runService starts the forwarding runtime as a Windows service. The full
// implementation is added in a later task; this placeholder keeps the build
// compilable during incremental extraction.
func runService() error {
	return nil
}
