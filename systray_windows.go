//go:build windows

package main

import (
	_ "embed"
	"syscall"
	"unsafe"
)

//go:embed assets/icon.png
var trayIconPNG []byte

const (
	wmDestroy   = 0x0002
	wmCommand   = 0x0111
	wmRButtonUp = 0x0205
	wmLButtonUp = 0x0202
	wmTrayIcon  = 0x8001 // WM_APP + 1, custom callback message for the tray icon
	wmClose     = 0x0010

	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	mfString = 0x00000000

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmBottomAlign = 0x0020

	idMenuOpen = 1001
	idMenuQuit = 1002

	imageIcon      = 1
	lrDefaultColor = 0x00000000
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW         = user32.NewProc("RegisterClassExW")
	procCreateWindowExW          = user32.NewProc("CreateWindowExW")
	procDefWindowProcW           = user32.NewProc("DefWindowProcW")
	procGetMessageW              = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessageW         = user32.NewProc("DispatchMessageW")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	procAppendMenuW              = user32.NewProc("AppendMenuW")
	procTrackPopupMenu           = user32.NewProc("TrackPopupMenu")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procDestroyMenu              = user32.NewProc("DestroyMenu")
	procCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	procPostMessageW             = user32.NewProc("PostMessageW")

	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type point struct {
	x, y int32
}

type msg struct {
	hwnd    syscall.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

// notifyIconData mirrors NOTIFYICONDATAW (the modern, post-XP layout).
type notifyIconData struct {
	cbSize            uint32
	hWnd              syscall.Handle
	uID               uint32
	uFlags            uint32
	uCallbackMessage  uint32
	hIcon             syscall.Handle
	szTip             [128]uint16
	dwState           uint32
	dwStateMask       uint32
	szInfo            [256]uint16
	uVersionOrTimeout uint32
	szInfoTitle       [64]uint16
	dwInfoFlags       uint32
	guidItem          [16]byte
	hBalloonIcon      syscall.Handle
}

// trayApp holds the running systray state.
type trayApp struct {
	hwnd   syscall.Handle
	icon   syscall.Handle
	onOpen func()
	onQuit func()
}

var activeTray *trayApp

// runSystray creates the tray icon and blocks in the Win32 message loop
// until Quit is selected or the window is destroyed. Call it from its own
// goroutine locked to the OS thread (see main.go).
func runSystray(tooltip string, onOpen func(), onQuit func()) error {
	className := syscall.StringToUTF16Ptr("PortalTrayWindowClass")

	hInstance, _, _ := procGetModuleHandleW.Call(0)

	wndProcPtr := syscall.NewCallback(trayWndProc)

	wc := wndClassExW{
		lpfnWndProc:   wndProcPtr,
		hInstance:     syscall.Handle(hInstance),
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// HWND_MESSAGE (-3): a message-only window, no visible UI needed.
	const hwndMessage = ^uintptr(2) // 0xFFFF...FFFD, i.e. (HWND)-3
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("portal"))),
		0, 0, 0, 0, 0,
		hwndMessage,
		0, hInstance, 0,
	)
	if hwnd == 0 {
		return syscall.GetLastError()
	}

	icon := loadTrayIcon()

	app := &trayApp{hwnd: syscall.Handle(hwnd), icon: icon, onOpen: onOpen, onQuit: onQuit}
	activeTray = app

	nid := notifyIconData{
		hWnd:             syscall.Handle(hwnd),
		uID:              1,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: wmTrayIcon,
		hIcon:            icon,
	}
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	copyStringToUTF16(nid.szTip[:], tooltip)

	procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	defer procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

// loadTrayIcon converts the embedded PNG into an HICON. CreateIconFromResourceEx
// accepts PNG-compressed icon image data directly on Vista and later.
func loadTrayIcon() syscall.Handle {
	if len(trayIconPNG) == 0 {
		return 0
	}
	h, _, _ := procCreateIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&trayIconPNG[0])),
		uintptr(len(trayIconPNG)),
		1, // fIcon = TRUE
		0x00030000,
		32, 32,
		lrDefaultColor,
	)
	return syscall.Handle(h)
}

func trayWndProc(hwnd syscall.Handle, message uint32, wParam, lParam uintptr) uintptr {
	app := activeTray

	switch message {
	case wmTrayIcon:
		switch uint32(lParam) {
		case wmLButtonUp, wmRButtonUp:
			if app != nil {
				showTrayMenu(hwnd)
			}
		}
		return 0

	case wmCommand:
		id := int(loWord(wParam))
		switch id {
		case idMenuOpen:
			if app != nil && app.onOpen != nil {
				app.onOpen()
			}
		case idMenuQuit:
			if app != nil && app.onQuit != nil {
				app.onQuit()
			}
			procPostQuitMessage.Call(0)
		}
		return 0

	case wmDestroy, wmClose:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return ret
}

func showTrayMenu(hwnd syscall.Handle) {
	hMenu, _, _ := procCreatePopupMenu.Call()
	defer procDestroyMenu.Call(hMenu)

	procAppendMenuW.Call(hMenu, mfString, idMenuOpen, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Open Dashboard"))))
	procAppendMenuW.Call(hMenu, mfString, idMenuQuit, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Quit"))))

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	// Required so the menu dismisses correctly when it loses focus.
	procSetForegroundWindow.Call(uintptr(hwnd))

	procTrackPopupMenu.Call(
		hMenu,
		tpmRightButton|tpmBottomAlign,
		uintptr(pt.x), uintptr(pt.y),
		0, uintptr(hwnd), 0,
	)
}

func loWord(v uintptr) uint16 {
	return uint16(v & 0xffff)
}

func copyStringToUTF16(dst []uint16, s string) {
	src := syscall.StringToUTF16(s)
	n := len(src)
	if n > len(dst) {
		n = len(dst)
	}
	copy(dst[:n], src[:n])
}
