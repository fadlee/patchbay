package main

import (
	"errors"
	"reflect"
	"testing"
)

func TestSelectTrayModeLocalWhenNotInstalled(t *testing.T) {
	if mode := selectTrayMode(serviceNotInstalled); mode != trayModeLocal {
		t.Fatalf("got %v, want trayModeLocal", mode)
	}
}

func TestSelectTrayModeClientWhenInstalled(t *testing.T) {
	if mode := selectTrayMode(serviceRunning); mode != trayModeClient {
		t.Fatalf("running service should select client mode, got %v", mode)
	}
	if mode := selectTrayMode(serviceStopped); mode != trayModeClient {
		t.Fatalf("stopped service should select client mode, got %v", mode)
	}
}

func TestEnableServiceReleasesLocalRuntimeBeforeServiceStart(t *testing.T) {
	var calls []string
	installFn := func(_ string) error { calls = append(calls, "install"); return nil }
	stopLocalFn := func() { calls = append(calls, "stop-local") }
	startFn := func() error { calls = append(calls, "start-service"); return nil }
	cleanupFn := func() error { calls = append(calls, "cleanup-service"); return nil }
	startLocalFn := func() error { calls = append(calls, "start-local"); return nil }

	if err := doEnableService(installFn, stopLocalFn, startFn, cleanupFn, startLocalFn, "test.exe"); err != nil {
		t.Fatalf("doEnableService: %v", err)
	}

	want := []string{"install", "stop-local", "start-service"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestEnableServiceRestoresLocalRuntimeAfterFailedStart(t *testing.T) {
	var calls []string
	installFn := func(_ string) error { calls = append(calls, "install"); return nil }
	stopLocalFn := func() { calls = append(calls, "stop-local") }
	startFn := func() error { calls = append(calls, "start-service"); return errors.New("bind failed") }
	cleanupFn := func() error { calls = append(calls, "cleanup-service"); return nil }
	startLocalFn := func() error { calls = append(calls, "start-local"); return nil }

	if err := doEnableService(installFn, stopLocalFn, startFn, cleanupFn, startLocalFn, "test.exe"); err == nil {
		t.Fatal("doEnableService should fail when service start fails")
	}

	want := []string{"install", "stop-local", "start-service", "cleanup-service", "start-local"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestEnableServiceDoesNotRestoreLocalRuntimeWhenCleanupFails(t *testing.T) {
	installFn := func(_ string) error { return nil }
	stopLocalFn := func() {}
	startFn := func() error { return errors.New("bind failed") }
	cleanupFn := func() error { return errors.New("service may still be running") }
	startLocalFn := func() error {
		t.Fatal("local runtime must not be restored while service cleanup fails")
		return nil
	}

	if err := doEnableService(installFn, stopLocalFn, startFn, cleanupFn, startLocalFn, "test.exe"); err == nil {
		t.Fatal("doEnableService should surface cleanup failure")
	}
}

func TestDisableServiceTransitionsToLocal(t *testing.T) {
	var calls []string
	stopFn := func() error { calls = append(calls, "stop"); return nil }
	deleteFn := func() error { calls = append(calls, "delete"); return nil }

	if err := doDisableService(serviceRunning, stopFn, deleteFn); err != nil {
		t.Fatalf("doDisableService: %v", err)
	}

	want := []string{"stop", "delete"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestDisableStoppedServiceDeletesWithoutStopping(t *testing.T) {
	stopFn := func() error {
		t.Fatal("stop should not be called for an already-stopped service")
		return nil
	}
	deleted := false
	deleteFn := func() error { deleted = true; return nil }

	if err := doDisableService(serviceStopped, stopFn, deleteFn); err != nil {
		t.Fatalf("doDisableService: %v", err)
	}
	if !deleted {
		t.Fatal("stopped service should still be deleted")
	}
}

func TestDisableFailureKeepsServiceClientMode(t *testing.T) {
	stopFn := func() error { return nil }
	deleteFn := func() error { return errors.New("access denied") }

	err := doDisableService(serviceRunning, stopFn, deleteFn)
	if err == nil {
		t.Fatal("doDisableService should fail when delete fails")
	}
}

func TestDisableServiceFailsOnStopError(t *testing.T) {
	stopFn := func() error { return errors.New("timeout") }
	deleteFn := func() error {
		t.Fatal("delete should not be called when stop fails")
		return nil
	}

	err := doDisableService(serviceRunning, stopFn, deleteFn)
	if err == nil {
		t.Fatal("doDisableService should fail when stop fails")
	}
}
