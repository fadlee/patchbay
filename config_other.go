//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func defaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return configFileName
	}
	return filepath.Join(filepath.Dir(exe), configFileName)
}

// prepareConfigPath resolves the effective config path and creates the parent
// directory. Non-Windows builds keep the executable-directory config location
// and do not migrate.
func prepareConfigPath(path string) (string, error) {
	if path == "" {
		path = defaultConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("create config directory: %w", err)
	}
	return path, nil
}
