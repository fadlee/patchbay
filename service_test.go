package main

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestServiceCommandLineQuotesExecutablePath(t *testing.T) {
	got := serviceCommandLine(`C:\Program Files\patchbay\patchbay.exe`)
	want := `"C:\Program Files\patchbay\patchbay.exe" service`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestServiceCommandLineSimplePath(t *testing.T) {
	got := serviceCommandLine(`C:\patchbay\patchbay.exe`)
	want := `"C:\patchbay\patchbay.exe" service`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestServiceCreateArgsSeparatesOptionsFromValues(t *testing.T) {
	got := serviceCreateArgs(`C:\Program Files\patchbay\patchbay.exe`)
	want := []string{
		"create",
		patchbayServiceName,
		"binpath=",
		`"C:\Program Files\patchbay\patchbay.exe" service`,
		"start=",
		"auto",
		"displayname=",
		patchbayDisplayName,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("serviceCreateArgs() = %#v, want %#v", got, want)
	}
}

func TestParseServiceStateRunning(t *testing.T) {
	output := []byte("SERVICE_NAME: PatchbayPortForwarder\n\tSTATE              : 4  RUNNING\n")
	state, err := parseServiceState(output, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != serviceRunning {
		t.Fatalf("got %v, want serviceRunning", state)
	}
}

func TestParseServiceStateStopped(t *testing.T) {
	output := []byte("SERVICE_NAME: PatchbayPortForwarder\n\tSTATE              : 1  STOPPED\n")
	state, err := parseServiceState(output, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != serviceStopped {
		t.Fatalf("got %v, want serviceStopped", state)
	}
}

func TestParseServiceStateStopPending(t *testing.T) {
	output := []byte("SERVICE_NAME: PatchbayPortForwarder\n\tSTATE              : 3  STOP_PENDING\n")
	state, err := parseServiceState(output, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != serviceStopPending {
		t.Fatalf("got %v, want serviceStopPending", state)
	}
}

func TestParseServiceStateStartPending(t *testing.T) {
	output := []byte("SERVICE_NAME: PatchbayPortForwarder\n\tSTATE              : 2  START_PENDING\n")
	state, err := parseServiceState(output, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != serviceStartPending {
		t.Fatalf("got %v, want serviceStartPending", state)
	}
}

func TestWaitForStateContinuesThroughStartPending(t *testing.T) {
	states := []serviceState{serviceStartPending, serviceRunning}
	calls := 0
	query := func() (serviceState, error) {
		state := states[calls]
		calls++
		return state, nil
	}

	if err := waitForState(serviceRunning, time.Second, query); err != nil {
		t.Fatalf("waitForState: %v", err)
	}
	if calls != 2 {
		t.Fatalf("query calls = %d, want 2", calls)
	}
}

func TestCleanupFailedServiceStartStopsRunningBeforeDelete(t *testing.T) {
	var calls []string
	query := func() (serviceState, error) { return serviceRunning, nil }
	stop := func() error { calls = append(calls, "stop"); return nil }
	delete := func() error { calls = append(calls, "delete"); return nil }

	if err := cleanupFailedServiceStart(query, stop, delete); err != nil {
		t.Fatalf("cleanupFailedServiceStart: %v", err)
	}
	if want := []string{"stop", "delete"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestCleanupFailedServiceStartDeletesStoppedService(t *testing.T) {
	query := func() (serviceState, error) { return serviceStopped, nil }
	stop := func() error {
		t.Fatal("stop should not be called for a stopped service")
		return nil
	}
	deleted := false
	delete := func() error { deleted = true; return nil }

	if err := cleanupFailedServiceStart(query, stop, delete); err != nil {
		t.Fatalf("cleanupFailedServiceStart: %v", err)
	}
	if !deleted {
		t.Fatal("stopped service should be deleted")
	}
}

func TestCleanupFailedServiceStartWaitsForStartPending(t *testing.T) {
	states := []serviceState{serviceStartPending, serviceStopped}
	calls := 0
	query := func() (serviceState, error) {
		state := states[calls]
		calls++
		return state, nil
	}
	stop := func() error {
		t.Fatal("stop should not be called when startup ends stopped")
		return nil
	}
	deleted := false
	delete := func() error { deleted = true; return nil }

	if err := cleanupFailedServiceStart(query, stop, delete); err != nil {
		t.Fatalf("cleanupFailedServiceStart: %v", err)
	}
	if calls != 2 || !deleted {
		t.Fatalf("query calls = %d, deleted = %t; want 2, true", calls, deleted)
	}
}

func TestWaitForStateContinuesThroughStopPending(t *testing.T) {
	states := []serviceState{serviceStopPending, serviceStopped}
	calls := 0
	query := func() (serviceState, error) {
		state := states[calls]
		calls++
		return state, nil
	}

	if err := waitForState(serviceStopped, time.Second, query); err != nil {
		t.Fatalf("waitForState: %v", err)
	}
	if calls != 2 {
		t.Fatalf("query calls = %d, want 2", calls)
	}
}

func TestWaitForStoppedAcceptsRemovedService(t *testing.T) {
	states := []serviceState{serviceStopPending, serviceNotInstalled}
	calls := 0
	query := func() (serviceState, error) {
		state := states[calls]
		calls++
		return state, nil
	}

	if err := waitForState(serviceStopped, time.Second, query); err != nil {
		t.Fatalf("waitForState: %v", err)
	}
	if calls != 2 {
		t.Fatalf("query calls = %d, want 2", calls)
	}
}

func TestParseServiceStateNotInstalled(t *testing.T) {
	output := []byte("[SC] EnumQueryServicesStatus:OpenService FAILED 1060:\nThe specified service does not exist as an installed service.\n")
	state, err := parseServiceState(output, errors.New("exit status 1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != serviceNotInstalled {
		t.Fatalf("got %v, want serviceNotInstalled", state)
	}
}

func TestParseServiceStateUnexpectedOutput(t *testing.T) {
	output := []byte("some random output")
	_, err := parseServiceState(output, nil)
	if err == nil {
		t.Fatal("expected error for unexpected output")
	}
}
