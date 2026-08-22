package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	patchbayServiceName = "PatchbayPortForwarder"
	patchbayDisplayName = "patchbay port forwarder"
	patchbayServiceDesc = "patchbay local dashboard and TCP/UDP forwarding service"
)

type serviceState int

const (
	serviceNotInstalled serviceState = iota
	serviceStopped
	serviceRunning
	serviceStopPending
)

func (s serviceState) String() string {
	switch s {
	case serviceNotInstalled:
		return "not installed"
	case serviceStopped:
		return "stopped"
	case serviceRunning:
		return "running"
	case serviceStopPending:
		return "stopping"
	default:
		return "unknown"
	}
}

// commandRunner abstracts subprocess execution so SCM operations can be tested
// without invoking the real sc.exe binary.
type commandRunner interface {
	Run(name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(name string, args ...string) ([]byte, error) {
	var buf bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

// serviceRunner is the default command runner used by SCM operations. Tests
// may replace it with a mock.
var serviceRunner commandRunner = execRunner{}

// serviceCommandLine returns the Windows service binPath value for the given
// executable: the path is double-quoted (so paths with spaces resolve) and the
// internal "service" argument is appended.
func serviceCommandLine(executable string) string {
	return fmt.Sprintf(`"%s" service`, executable)
}

// serviceCreateArgs returns sc.exe create arguments with every option and
// value as separate tokens. sc.exe rejects combined tokens such as
// "start= auto" with ERROR_INVALID_COMMAND_LINE (1639).
func serviceCreateArgs(executable string) []string {
	return []string{
		"create",
		patchbayServiceName,
		"binpath=",
		serviceCommandLine(executable),
		"start=",
		"auto",
		"displayname=",
		patchbayDisplayName,
	}
}

// parseServiceState interprets sc.exe query output to determine the current
// service state. When sc.exe reports that the service does not exist (error
// 1060), the state is serviceNotInstalled rather than an error.
func parseServiceState(output []byte, err error) (serviceState, error) {
	s := string(output)
	if err != nil {
		if strings.Contains(s, "does not exist") || strings.Contains(s, "1060") {
			return serviceNotInstalled, nil
		}
		return serviceNotInstalled, fmt.Errorf("sc query failed: %w: %s", err, s)
	}
	if strings.Contains(s, "RUNNING") {
		return serviceRunning, nil
	}
	if strings.Contains(s, "STOP_PENDING") {
		return serviceStopPending, nil
	}
	if strings.Contains(s, "STOPPED") {
		return serviceStopped, nil
	}
	return serviceNotInstalled, fmt.Errorf("unexpected sc query output: %s", s)
}

// waitForState polls query until the target state is observed or the timeout
// elapses. The query function is injected so the helper is testable without
// real SCM.
func waitForState(target serviceState, timeout time.Duration, query func() (serviceState, error)) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, err := query()
		if err != nil {
			return err
		}
		if state == target {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for service to become %s", target)
}
