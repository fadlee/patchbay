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

const startupRunKey = `HKLM\Software\Microsoft\Windows\CurrentVersion\Run`
const startupValueName = "PatchbayTray"

func installService(executable string) error {
	if _, err := serviceRunner.Run("sc.exe", serviceCreateArgs(executable)...); err != nil {
		return fmt.Errorf("sc create failed: %w", err)
	}
	if _, err := serviceRunner.Run("sc.exe", "description", patchbayServiceName, patchbayServiceDesc); err != nil {
		return fmt.Errorf("sc description failed: %w", err)
	}
	// Register tray executable to start on user login so tray icon is visible after reboot
	_, _ = serviceRunner.Run("reg.exe", "add", startupRunKey, "/v", startupValueName, "/t", "REG_SZ", "/d", fmt.Sprintf(`"%s"`, executable), "/f")
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
	// Remove tray startup registry
	_, _ = serviceRunner.Run("reg.exe", "delete", startupRunKey, "/v", startupValueName, "/f")
	return nil
}
