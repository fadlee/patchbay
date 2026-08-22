package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigMigrationCopiesAdjacentConfigToDestination(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src-config.json")
	dst := filepath.Join(t.TempDir(), "dst-dir", "dst-config.json")

	original := Config{AdminPort: 9999, Rules: []Rule{{ID: "test", Name: "Test", Enabled: true}}}
	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("marshal source: %v", err)
	}
	if err := os.WriteFile(src, data, 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := migrateConfig(src, dst); err != nil {
		t.Fatalf("migrateConfig: %v", err)
	}

	// Source must be preserved untouched.
	srcData, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read source after migration: %v", err)
	}
	if string(srcData) != string(data) {
		t.Fatal("source config was modified during migration")
	}

	// Destination contents must equal source.
	dstData, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	var dstCfg Config
	if err := json.Unmarshal(dstData, &dstCfg); err != nil {
		t.Fatalf("parse destination: %v", err)
	}
	if dstCfg.AdminPort != 9999 || len(dstCfg.Rules) != 1 || dstCfg.Rules[0].ID != "test" {
		t.Fatalf("destination config mismatch: %+v", dstCfg)
	}
}

func TestConfigMigrationDoesNotOverwriteExistingDestination(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src-config.json")
	dst := filepath.Join(t.TempDir(), "dst-config.json")

	srcCfg := Config{AdminPort: 1111}
	srcData, _ := json.MarshalIndent(srcCfg, "", "  ")
	if err := os.WriteFile(src, srcData, 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	dstCfg := Config{AdminPort: 2222}
	dstData, _ := json.MarshalIndent(dstCfg, "", "  ")
	if err := os.WriteFile(dst, dstData, 0644); err != nil {
		t.Fatalf("write destination: %v", err)
	}

	if err := migrateConfig(src, dst); err != nil {
		t.Fatalf("migrateConfig: %v", err)
	}

	result, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	var resultCfg Config
	if err := json.Unmarshal(result, &resultCfg); err != nil {
		t.Fatalf("parse destination: %v", err)
	}
	if resultCfg.AdminPort != 2222 {
		t.Fatalf("destination was overwritten: got %d, want 2222", resultCfg.AdminPort)
	}
}

func TestConfigMigrationRejectsInvalidSource(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src-config.json")
	dst := filepath.Join(t.TempDir(), "dst-dir", "dst-config.json")

	if err := os.WriteFile(src, []byte("{invalid json"), 0644); err != nil {
		t.Fatalf("write invalid source: %v", err)
	}

	err := migrateConfig(src, dst)
	if err == nil {
		t.Fatal("migrateConfig should fail for invalid source")
	}

	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Fatalf("destination should not exist after failed migration: %v", statErr)
	}
}

func TestConfigMigrationNoOpWhenSourceMissing(t *testing.T) {
	src := filepath.Join(t.TempDir(), "nonexistent.json")
	dst := filepath.Join(t.TempDir(), "dst-config.json")

	if err := migrateConfig(src, dst); err != nil {
		t.Fatalf("migrateConfig with missing source should be no-op: %v", err)
	}

	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Fatalf("destination should not be created when source is missing: %v", statErr)
	}
}

func TestNewConfigStoreCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deep", "config.json")
	store, err := NewConfigStore(path)
	if err != nil {
		t.Fatalf("NewConfigStore: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file should exist: %v", err)
	}

	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("parent directory should exist: %v", err)
	}

	cfg := store.Snapshot()
	if cfg.AdminPort != 8787 {
		t.Fatalf("default AdminPort = %d, want 8787", cfg.AdminPort)
	}
}
