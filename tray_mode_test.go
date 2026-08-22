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

func TestEnableServiceTransitionsTrayToClient(t *testing.T) {
	var calls []string
	installFn := func(_ string) error { calls = append(calls, "install"); return nil }
	startFn := func() error { calls = append(calls, "start"); return nil }

	if err := doEnableService(installFn, startFn, "test.exe"); err != nil {
		t.Fatalf("doEnableService: %v", err)
	}

	want := []string{"install", "start"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestEnableServiceFailsOnInstallError(t *testing.T) {
	installFn := func(_ string) error { return errors.New("access denied") }
	startFn := func() error {
		t.Fatal("start should not be called when install fails")
		return nil
	}

	err := doEnableService(installFn, startFn, "test.exe")
	if err == nil {
		t.Fatal("doEnableService should fail when install fails")
	}
}

func TestDisableServiceTransitionsToLocal(t *testing.T) {
	var calls []string
	stopFn := func() error { calls = append(calls, "stop"); return nil }
	deleteFn := func() error { calls = append(calls, "delete"); return nil }

	if err := doDisableService(stopFn, deleteFn); err != nil {
		t.Fatalf("doDisableService: %v", err)
	}

	want := []string{"stop", "delete"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestDisableFailureKeepsServiceClientMode(t *testing.T) {
	stopFn := func() error { return nil }
	deleteFn := func() error { return errors.New("access denied") }

	err := doDisableService(stopFn, deleteFn)
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

	err := doDisableService(stopFn, deleteFn)
	if err == nil {
		t.Fatal("doDisableService should fail when stop fails")
	}
}
