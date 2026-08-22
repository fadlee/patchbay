//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func defaultConfigPath() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "patchbay", configFileName)
}

// adjacentConfigPath returns the legacy executable-directory config path used
// as the migration source when the shared ProgramData config does not yet exist.
func adjacentConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), configFileName)
}

// prepareConfigPath resolves the effective config path, creates the parent
// directory, and migrates an adjacent executable config into the shared
// ProgramData location on first run.
func prepareConfigPath(path string) (string, error) {
	if path == "" {
		path = defaultConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("create config directory: %w", err)
	}
	if src := adjacentConfigPath(); src != "" && src != path {
		if err := migrateConfig(src, path); err != nil {
			return "", err
		}
	}
	return path, nil
}
