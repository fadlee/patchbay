//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

func defaultLogDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "logs"
	}
	return filepath.Join(filepath.Dir(exe), "logs")
}
