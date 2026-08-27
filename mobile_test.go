package main

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestMobileLifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "patchbay-mobile-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	if IsMobileRunning() {
		t.Fatalf("expected not running initially")
	}

	// Start with custom port
	adminPort := 18788
	if err := StartMobile(tempDir, "127.0.0.1", adminPort); err != nil {
		t.Fatalf("StartMobile: %v", err)
	}

	if !IsMobileRunning() {
		t.Fatalf("expected running after StartMobile")
	}

	expectedURL := fmt.Sprintf("http://127.0.0.1:%d/", adminPort)
	if url := GetMobileDashboardURL(); url != expectedURL {
		t.Fatalf("expected url %s, got %s", expectedURL, url)
	}

	// Verify dashboard responds
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(expectedURL)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	// Try double-start, should fail
	if err := StartMobile(tempDir, "127.0.0.1", adminPort); err == nil {
		t.Fatalf("expected error on duplicate StartMobile, got nil")
	}

	// Stop
	StopMobile()

	if IsMobileRunning() {
		t.Fatalf("expected not running after StopMobile")
	}
	if url := GetMobileDashboardURL(); url != "" {
		t.Fatalf("expected empty url after stop, got %s", url)
	}
}
