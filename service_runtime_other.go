//go:build !windows

package main

import "errors"

// runService is a stub on non-Windows platforms. Service mode is Windows-only.
func runService() error {
	return errors.New("service mode is not supported on this platform")
}
