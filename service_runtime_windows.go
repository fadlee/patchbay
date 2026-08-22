//go:build windows

package main

import (
	"context"
	"log"
	"syscall"
	"unsafe"
)

var (
	advapi32                          = syscall.NewLazyDLL("advapi32.dll")
	procStartServiceCtrlDispatcherW   = advapi32.NewProc("StartServiceCtrlDispatcherW")
	procRegisterServiceCtrlHandlerExW = advapi32.NewProc("RegisterServiceCtrlHandlerExW")
	procSetServiceStatus              = advapi32.NewProc("SetServiceStatus")
)

const (
	serviceWin32OwnProcess = 0x00000010
	scmStatusStopped       = 0x00000001
	scmStatusStartPending  = 0x00000002
	scmStatusStopPending   = 0x00000003
	scmStatusRunning       = 0x00000004
	scmAcceptStop          = 0x00000001
	scmAcceptShutdown      = 0x00000004

	scmControlStop     = 0x00000001
	scmControlShutdown = 0x00000005
)
type serviceTableEntry struct {
	serviceName *uint16
	serviceProc uintptr
}

type serviceStatus struct {
	serviceType             uint32
	currentState            uint32
	controlsAccepted        uint32
	win32ExitCode           uint32
	serviceSpecificExitCode uint32
	checkPoint              uint32
	waitHint                uint32
}

var (
	scmStatusHandle uintptr
	scmStopCh       chan struct{}
)

// runService connects to the Windows Service Control Manager and runs the
// forwarding runtime as a service. It blocks until the service is stopped.
func runService() error {
	name := syscall.StringToUTF16Ptr(patchbayServiceName)
	entry := serviceTableEntry{
		serviceName: name,
		serviceProc: syscall.NewCallback(serviceMain),
	}
	table := []serviceTableEntry{entry, {nil, 0}}

	ret, _, err := procStartServiceCtrlDispatcherW.Call(uintptr(unsafe.Pointer(&table[0])))
	if ret == 0 {
		return err
	}
	return nil
}

func serviceMain(argc uint32, argv uintptr) uintptr {
	scmStopCh = make(chan struct{})

	name := syscall.StringToUTF16Ptr(patchbayServiceName)
	handle, _, _ := procRegisterServiceCtrlHandlerExW.Call(
		uintptr(unsafe.Pointer(name)),
		syscall.NewCallback(serviceCtrlHandler),
		0,
	)
	if handle == 0 {
		log.Printf("service: RegisterServiceCtrlHandler failed")
		return 1
	}
	scmStatusHandle = handle

	reportServiceStatus(scmStatusStartPending, 0, 0, 1000)

	store, err := NewConfigStore("")
	if err != nil {
		log.Printf("service: failed to load config: %v", err)
		reportServiceStatus(scmStatusStopped, 1, 0, 0)
		return 1
	}

	manager := NewManager()
	rt := newRuntime(store, manager)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rt.start(ctx); err != nil {
		log.Printf("service: failed to start runtime: %v", err)
		reportServiceStatus(scmStatusStopped, 1, 0, 0)
		return 1
	}

	reportServiceStatus(scmStatusRunning, 0, scmAcceptStop|scmAcceptShutdown, 0)

	<-scmStopCh

	reportServiceStatus(scmStatusStopPending, 0, 0, 5000)
	rt.stop()
	reportServiceStatus(scmStatusStopped, 0, 0, 0)

	return 0
}

func serviceCtrlHandler(ctrl, eventType, eventData, context uintptr) uintptr {
	switch uint32(ctrl) {
	case scmControlStop, scmControlShutdown:
		if scmStopCh != nil {
			close(scmStopCh)
			scmStopCh = nil
		}
	}
	return 0
}

func reportServiceStatus(state, exitCode, controlsAccepted, waitHint uint32) {
	if scmStatusHandle == 0 {
		return
	}
	status := serviceStatus{
		serviceType:      serviceWin32OwnProcess,
		currentState:     state,
		controlsAccepted: controlsAccepted,
		win32ExitCode:    exitCode,
		checkPoint:       0,
		waitHint:         waitHint,
	}
	procSetServiceStatus.Call(scmStatusHandle, uintptr(unsafe.Pointer(&status)))
}
