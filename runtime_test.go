package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestConfig(t *testing.T, path string, cfg Config) {
	t.Helper()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestRuntimeStartsOnlyEnabledRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen target: %v", err)
	}
	defer target.Close()
	targetPort := target.Addr().(*net.TCPAddr).Port

	writeTestConfig(t, path, Config{
		AdminPort: freeTCPPort(t),
		Rules: []Rule{
			{ID: "enabled-rule", Name: "Enabled", Protocol: "tcp", ListenAddr: "127.0.0.1", ListenPort: freeTCPPort(t), TargetAddr: "127.0.0.1", TargetPort: targetPort, Enabled: true},
			{ID: "disabled-rule", Name: "Disabled", Protocol: "tcp", ListenAddr: "127.0.0.1", ListenPort: freeTCPPort(t), TargetAddr: "127.0.0.1", TargetPort: targetPort, Enabled: false},
		},
	})

	store, err := NewConfigStore(path)
	if err != nil {
		t.Fatalf("NewConfigStore: %v", err)
	}

	manager := NewManager()
	rt := newRuntime(store, manager)

	if err := rt.start(context.Background()); err != nil {
		t.Fatalf("runtime start: %v", err)
	}
	defer rt.stop()

	if !manager.IsRunning("enabled-rule") {
		t.Fatal("enabled rule should be running")
	}
	if manager.IsRunning("disabled-rule") {
		t.Fatal("disabled rule should not be running")
	}
}

func TestRuntimeStopClosesHTTPAndForwardingRuntime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen target: %v", err)
	}
	defer target.Close()
	targetPort := target.Addr().(*net.TCPAddr).Port

	adminPort := freeTCPPort(t)
	writeTestConfig(t, path, Config{
		AdminPort: adminPort,
		Rules: []Rule{
			{ID: "test-rule", Name: "Test", Protocol: "tcp", ListenAddr: "127.0.0.1", ListenPort: freeTCPPort(t), TargetAddr: "127.0.0.1", TargetPort: targetPort, Enabled: true},
		},
	})

	store, err := NewConfigStore(path)
	if err != nil {
		t.Fatalf("NewConfigStore: %v", err)
	}

	manager := NewManager()
	rt := newRuntime(store, manager)

	if err := rt.start(context.Background()); err != nil {
		t.Fatalf("runtime start: %v", err)
	}

	// Verify HTTP dashboard is serving.
	dashboardURL := fmt.Sprintf("http://127.0.0.1:%d/", adminPort)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(dashboardURL)
	if err != nil {
		t.Fatalf("dashboard not serving: %v", err)
	}
	resp.Body.Close()

	if !manager.IsRunning("test-rule") {
		t.Fatal("rule should be running before stop")
	}

	// Stop the runtime.
	rt.stop()

	// Verify HTTP listener is closed.
	_, err = client.Get(dashboardURL)
	if err == nil {
		t.Fatal("dashboard should be closed after stop")
	}

	// Verify forwarding is stopped.
	if manager.IsRunning("test-rule") {
		t.Fatal("rule should be stopped after runtime stop")
	}
}
