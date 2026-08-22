//go:build !windows

package main

import "errors"

func queryService() (serviceState, error) {
	return serviceNotInstalled, nil
}

func installService(executable string) error {
	return errors.New("service mode is not supported on this platform")
}

func startService() error {
	return errors.New("service mode is not supported on this platform")
}

func stopService() error {
	return errors.New("service mode is not supported on this platform")
}

func deleteService() error {
	return errors.New("service mode is not supported on this platform")
}
