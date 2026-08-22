package main

import (
	"testing"
)

func TestDispatchModeService(t *testing.T) {
	var serviceCalled, trayCalled bool
	serviceFn := func() error { serviceCalled = true; return nil }
	trayFn := func() error { trayCalled = true; return nil }

	if err := dispatchMode([]string{"patchbay.exe", "service"}, serviceFn, trayFn); err != nil {
		t.Fatalf("dispatchMode: %v", err)
	}
	if !serviceCalled {
		t.Fatal("service function should be called for service arg")
	}
	if trayCalled {
		t.Fatal("tray function should not be called for service arg")
	}
}

func TestDispatchModeTray(t *testing.T) {
	var serviceCalled, trayCalled bool
	serviceFn := func() error { serviceCalled = true; return nil }
	trayFn := func() error { trayCalled = true; return nil }

	if err := dispatchMode([]string{"patchbay.exe"}, serviceFn, trayFn); err != nil {
		t.Fatalf("dispatchMode: %v", err)
	}
	if serviceCalled {
		t.Fatal("service function should not be called without service arg")
	}
	if !trayCalled {
		t.Fatal("tray function should be called without service arg")
	}
}

func TestDispatchModeExtraArgs(t *testing.T) {
	var serviceCalled, trayCalled bool
	serviceFn := func() error { serviceCalled = true; return nil }
	trayFn := func() error { trayCalled = true; return nil }

	// Only args[1] == "service" triggers service mode; other args go to tray.
	if err := dispatchMode([]string{"patchbay.exe", "--help"}, serviceFn, trayFn); err != nil {
		t.Fatalf("dispatchMode: %v", err)
	}
	if serviceCalled {
		t.Fatal("service function should not be called for --help")
	}
	if !trayCalled {
		t.Fatal("tray function should be called for --help")
	}
}
