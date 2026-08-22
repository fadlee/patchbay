//go:build windows

package main

import (
	"os"
	"path/filepath"
)

func defaultLogDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "patchbay", "logs")
}
