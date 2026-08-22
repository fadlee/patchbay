//go:build windows

package main

import (
	"fmt"
	"time"
)

func queryService() (serviceState, error) {
	out, err := serviceRunner.Run("sc.exe", "query", patchbayServiceName)
	return parseServiceState(out, err)
}

func installService(executable string) error {
	binPath := serviceCommandLine(executable)
	if _, err := serviceRunner.Run("sc.exe", "create", patchbayServiceName,
		fmt.Sprintf("binpath= %s", binPath),
		"start= auto",
		fmt.Sprintf("displayname= %s", patchbayDisplayName)); err != nil {
		return fmt.Errorf("sc create failed: %w", err)
	}
	if _, err := serviceRunner.Run("sc.exe", "description", patchbayServiceName, patchbayServiceDesc); err != nil {
		return fmt.Errorf("sc description failed: %w", err)
	}
	return nil
}

func startService() error {
	if _, err := serviceRunner.Run("sc.exe", "start", patchbayServiceName); err != nil {
		return fmt.Errorf("sc start failed: %w", err)
	}
	return waitForState(serviceRunning, 15*time.Second, queryService)
}

func stopService() error {
	if _, err := serviceRunner.Run("sc.exe", "stop", patchbayServiceName); err != nil {
		return fmt.Errorf("sc stop failed: %w", err)
	}
	return waitForState(serviceStopped, 15*time.Second, queryService)
}

func deleteService() error {
	if _, err := serviceRunner.Run("sc.exe", "delete", patchbayServiceName); err != nil {
		return fmt.Errorf("sc delete failed: %w", err)
	}
	return nil
}
