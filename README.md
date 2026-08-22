# patchbay

Modern TCP/UDP port forwarder for Windows, inspired by AUTAPF (which is
discontinued). Single `.exe`, no installer, no runtime dependencies — a
Go binary with an embedded HTMX web dashboard and a native system tray icon.

## Features

- TCP and UDP forwarding (or both on the same rule)
- Web dashboard (HTMX) to add/remove/start/stop rules, with live
  active-connection and total-connection counts, refreshed every 3s
- System tray icon → **Open Dashboard** / **Quit**
- Optional Windows Service mode: forwarding and dashboard survive reboots
  without requiring a user login session
- Rules persist to a shared config (`%ProgramData%\patchbay` on Windows)
- Zero external dependencies — everything (including the tray icon, which
  talks to Win32 directly via `syscall`) is Go stdlib

## Build

Requires Go 1.22+.

```bash
# Windows binary, no console window, single file:
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o patchbay.exe -ldflags="-H windowsgui" .
```

For local development/testing on Linux or macOS, just `go build .` — the
systray becomes a no-op stub (see `systray_other.go`) and the dashboard is
still reachable at `http://127.0.0.1:8787`.

## Run

Double-click `patchbay.exe`. It starts silently in the tray — right-click
the icon for **Open Dashboard** or **Quit**. The dashboard defaults to
`http://127.0.0.1:8787`.

On Windows, config/rules are stored in
`%ProgramData%\patchbay\portforward-config.json`. On first run, an existing
`portforward-config.json` next to the executable is migrated there. On
non-Windows, the config stays next to the executable for development.

## Windows Service Mode

Service mode lets forwarding and the dashboard start automatically at boot,
before any user logs in. The same executable serves as both the service
process and the tray UI.

**Enable:** Right-click the tray icon → **Enable service mode**. UAC prompts
for elevation, the service is installed with automatic startup, and started.
The tray becomes a UI-only client — it opens the service-owned dashboard and
sends rule changes through the same loopback HTTP API.

**After reboot:** The service starts automatically and resumes all rules
with `Enabled == true` from the persisted config.

**Quit:** In service-client mode, **Quit** exits only the tray process;
forwarding and the dashboard keep running under the service.

**Disable:** Right-click → **Disable service mode**. The service is stopped
and removed; the tray returns to local mode and starts its own forwarding
runtime. If stop or removal fails, the tray stays in client mode and shows
the error.

The service name is `PatchbayPortForwarder`. Service management uses
`sc.exe` directly — no third-party dependencies.

## Project layout

| File                    | What it does                                             |
|-------------------------|-----------------------------------------------------------|
| `main.go`                | entry point: dispatches service/tray mode, tray orchestration |
| `config.go`               | `Rule`/`Config` model, thread-safe JSON load/save          |
| `config_windows.go`       | Windows shared config path (`%ProgramData%`) + migration   |
| `config_other.go`         | non-Windows config path (executable-directory)             |
| `proxy.go`                 | the actual TCP/UDP forwarding engine + live stats         |
| `runtime.go`              | shared cancellable runtime: manager + HTTP dashboard      |
| `server.go`                | HTTP handlers (HTMX partials) for the dashboard            |
| `service.go`              | SCM types, command construction, state parsing            |
| `service_windows.go`      | `sc.exe`-based service install/start/stop/delete          |
| `service_other.go`        | non-Windows service stubs                                 |
| `service_runtime_windows.go` | Windows service entrypoint (SCM dispatcher)            |
| `service_runtime_other.go`   | non-Windows `runService` stub                          |
| `tray_mode.go`            | tray mode selection and service enable/disable flows      |
| `systray_windows.go`       | tray icon, built only for `GOOS=windows`, raw Win32 syscalls |
| `systray_other.go`         | no-op stub so the project builds on other OSes for dev     |
| `web/templates/*.html`     | dashboard HTML (Go `html/template` + HTMX)                 |
| `web/static/`              | `htmx.min.js` (vendored) + `style.css`                    |
| `assets/icon.png`          | tray icon (32×32), embedded into the binary                |

## Ideas for next steps

- Auth on the dashboard (it's currently open to anyone who can reach
  `127.0.0.1:8787` — fine for local/admin use, not for exposing the admin
  port itself to a network)
- Per-rule bandwidth graphs (the `Stats` struct already tracks bytes
  in/out, just needs a chart)
- Config import/export from the UI
