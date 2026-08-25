# patchbay

Modern TCP/UDP port forwarder and lightweight HAProxy alternative with Web UI.
Available as a standalone Windows app/service or as a **Docker container** for Linux servers.
Built in Go with an embedded Vanilla JS web dashboard, Server-Sent Events (SSE) realtime
streaming, live traffic logging, and automatic update capabilities via GitHub Releases.

## Features
- **TCP and UDP forwarding** (or both on the same rule)
- **Realtime web dashboard** via **Server-Sent Events (SSE)** and native **Vanilla JS**
- **Live bandwidth & throughput chart** rendered via **HTML5 Canvas** (0 external libraries)
- **Live connection session logger** with in-memory ring buffer and daily rotating file persistence (`.jsonl`)
- **Native system tray icon**: **Open Dashboard**, **Check for Updates...**, **Service controls**, **Quit**
- **Optional Windows Service mode**: forwarding and dashboard survive reboots without requiring an active user session
- **Built-in Auto-Update**: automatically checks latest GitHub releases with one-click update & restart in dashboard and tray
- **Configuration persistence**: shared config in `%ProgramData%\patchbay` on Windows
- **Zero runtime external dependencies**: everything (including tray icon & Win32 service handling) is built using Go stdlib & native Win32 APIs
## Build

Requires Go 1.22+ and [Task](https://taskfile.dev).

```bash
# Build standalone Windows binary (embedded manifest & tray icon):
task build-windows

# Build Windows NSIS setup installer (dist/patchbay-setup-amd64.exe):
# Requires NSIS (e.g. `sudo apt install nsis` on Linux / WSL)
task build-installer

# Build local dev binary on Linux / macOS:
task build
```
## Run

Double-click `patchbay.exe`. It starts silently in the tray — right-click
the icon for **Open Dashboard** or **Quit**. The dashboard defaults to
`http://127.0.0.1:8787`.

On Windows, config/rules and traffic logs are stored in:
- Config: `%ProgramData%\patchbay\portforward-config.json`
- Logs: `%ProgramData%\patchbay\logs\traffic-YYYY-MM-DD.jsonl`

On first run, an existing `portforward-config.json` next to the executable
is migrated there. On non-Windows, the config and logs stay adjacent to the
executable for development.

## Run with Docker (Linux / Server)

Patchbay can run seamlessly in Docker as a lightweight, visual port forwarder.

### Option 1: Host Networking (Recommended to manage any host port)
Using `network_mode: host` allows Patchbay to bind directly to any port on the host machine without needing to pre-publish `-p <port>:<port>` in Docker beforehand:

```yaml
# docker-compose.yml
services:
  patchbay:
    image: ghcr.io/fadlee/patchbay:latest
    container_name: patchbay
    restart: unless-stopped
    network_mode: host
    volumes:
      - ./data:/app
    environment:
      - PATCHBAY_HOST=0.0.0.0 # Binds dashboard to all network interfaces
```

Or via `docker run`:
```bash
docker run -d \
  --name patchbay \
  --restart unless-stopped \
  --network host \
  -v $(pwd)/data:/app \
  -e PATCHBAY_HOST=0.0.0.0 \
  ghcr.io/fadlee/patchbay:latest
```
> **Note:** Dengan mode `host`, setiap aturan forward yang Anda tambahkan di Web UI (`http://<server-ip>:8787`) akan langsung membuka dan mendengarkan port tersebut di host Linux Anda secara dinamis.

---

### Option 2: Bridge Networking (Port Mapping)
Jika tidak ingin menggunakan host network, gunakan port mapping standar:

```yaml
services:
  patchbay:
    image: ghcr.io/fadlee/patchbay:latest
    container_name: patchbay
    restart: unless-stopped
    ports:
      - "8787:8787" # Admin Dashboard
      # Daftarkan port forwarding yang ingin dibuka ke host:
      - "8080:8080"
      - "5433:5433"
    volumes:
      - ./data:/app
    environment:
      - PATCHBAY_HOST=0.0.0.0
```

---

### Target Host / Container Routing
- Untuk forward ke service di host machine dari dalam container bridge: gunakan target `host.docker.internal` (dengan `extra_hosts: ["host.docker.internal:host-gateway"]`).
- Jika menggunakan `network_mode: host`, target ke service lokal di mesin host cukup menggunakan `127.0.0.1`.

---


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

| File                       | What it does                                                 |
|----------------------------|--------------------------------------------------------------|
| `main.go`                  | entry point: dispatches service/tray mode, tray orchestration |
| `updater.go`               | GitHub Releases API client, SemVer comparison, installer download |
| `config.go`                | `Rule`/`Config` model, thread-safe JSON load/save             |
| `config_windows.go`        | Windows shared config path (`%ProgramData%`) + migration      |
| `config_other.go`          | non-Windows config path (executable-directory)                |
| `logger.go`                | in-memory ring buffer, daily JSONL logger, traffic summary   |
| `logger_windows.go`        | Windows shared log directory path (`%ProgramData%\logs`)     |
| `logger_other.go`          | non-Windows log directory path (`./logs`)                    |
| `proxy.go`                 | TCP/UDP forwarding engine + session-level connection logger  |
| `runtime.go`               | shared cancellable runtime: manager, SSE stream, dashboard   |
| `server.go`                | REST JSON API routes and web asset server                    |
| `sse.go`                   | Server-Sent Events broadcaster and subscriber manager        |
| `service.go`               | SCM types, command construction, state parsing               |
| `service_windows.go`       | `sc.exe`-based service install/start/stop/delete             |
| `service_other.go`         | non-Windows service stubs                                    |
| `service_runtime_windows.go` | Windows service entrypoint (SCM dispatcher)               |
| `service_runtime_other.go` | non-Windows `runService` stub                                |
| `tray_mode.go`             | tray mode selection and service enable/disable flows         |
| `systray_windows.go`       | tray icon, built only for `GOOS=windows`, raw Win32 syscalls    |
| `systray_other.go`         | no-op stub so the project builds on other OSes for dev        |
| `web/templates/index.html` | dashboard single-page HTML layout with update banner         |
| `web/static/app.js`        | Vanilla JS client: SSE consumer, Canvas chart, REST actions  |
| `web/static/style.css`     | lightweight stylesheet (light & dark theme)                  |
| `assets/icon.png`          | tray icon (32×32), embedded into the binary                   |
| `.github/workflows/release.yml` | GitHub Actions CI/CD for automated build & release        |
## Ideas for next steps

- Auth on the dashboard (it's currently open to anyone who can reach
  `127.0.0.1:8787` — fine for local/admin use, not for exposing the admin
  port itself to a network)
- Config import/export from the UI
