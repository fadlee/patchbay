# patchbay

Modern TCP/UDP port forwarder for Windows, inspired by AUTAPF (which is
discontinued). Single `.exe`, no installer, no runtime dependencies — a
Go binary with an embedded HTMX web dashboard and a native system tray icon.

## Features

- TCP and UDP forwarding (or both on the same rule)
- Web dashboard (HTMX) to add/remove/start/stop rules, with live
  active-connection and total-connection counts, refreshed every 3s
- System tray icon → **Open Dashboard** / **Quit**
- Rules persist to `portforward-config.json` next to the executable
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

Config/rules are stored in `portforward-config.json`, created next to the
exe on first run.

## Project layout

| File                    | What it does                                             |
|-------------------------|-----------------------------------------------------------|
| `main.go`                | wiring: config → manager → HTTP server → systray          |
| `config.go`               | `Rule`/`Config` model, thread-safe JSON load/save          |
| `proxy.go`                 | the actual TCP/UDP forwarding engine + live stats         |
| `server.go`                | HTTP handlers (HTMX partials) for the dashboard            |
| `systray_windows.go`       | tray icon, built only for `GOOS=windows`, raw Win32 syscalls |
| `systray_other.go`         | no-op stub so the project builds on other OSes for dev     |
| `web/templates/*.html`     | dashboard HTML (Go `html/template` + HTMX)                 |
| `web/static/`              | `htmx.min.js` (vendored) + `style.css`                    |
| `assets/icon.png`          | tray icon (32×32), embedded into the binary                |

## Ideas for next steps

- Windows Service mode (`kardianos/service`-style) so it survives reboots
  without the tray/login-session requirement
- Auth on the dashboard (it's currently open to anyone who can reach
  `127.0.0.1:8787` — fine for local/admin use, not for exposing the admin
  port itself to a network)
- Per-rule bandwidth graphs (the `Stats` struct already tracks bytes
  in/out, just needs a chart)
- Config import/export from the UI
- Firewall rule auto-creation (`netsh advfirewall`) when a rule is added
