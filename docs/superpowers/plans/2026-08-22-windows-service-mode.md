# Windows Service Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an optional Windows Service mode where the service owns forwarding and the localhost dashboard across reboots, while the tray becomes a UI-only controller.

**Architecture:** Keep one executable with two Windows entry modes: the normal user-session mode and the `service` mode invoked by Service Control Manager. Extract shared runtime construction and shutdown from `main.go`; use `%ProgramData%\\patchbay\\portforward-config.json` for Windows shared state; use `sc.exe` from the elevated tray to install, start, stop, and remove the service. The tray queries SCM state and never starts a local forwarding/dashboard runtime when the service is installed.

**Tech Stack:** Go 1.22+, standard library, raw Windows Service Control Manager APIs via `syscall`/`NewLazyDLL`, `sc.exe`, existing embedded Go HTTP/HTMX dashboard, Taskfile, Windows manifest.

**Spec:** `docs/service-mode-design.md`

## Global Constraints

- Service mode is Windows-only; non-Windows builds retain the current development behavior.
- `Rule.Enabled` remains the persisted desired state for each rule; SCM state is separate.
- The service is the only owner of forwarding listeners and the dashboard while installed.
- Tray-to-service rule control uses the existing loopback HTTP API without authentication.
- Service management uses `sc.exe`; do not add a third-party service dependency.
- The existing `requireAdministrator` manifest supplies elevation for the Windows tray process.
- Windows config path is `%ProgramData%\\patchbay\\portforward-config.json`; non-Windows keeps executable-directory behavior.
- Service lifecycle operations must report success only after the requested SCM state is observed, with bounded waits.
- Every implementation task writes a failing behavioral test first, runs it, then adds the minimal implementation.
- Skip formatters/linters/project-wide suites during individual task work; run the full verification set only in the final task.

---

### Task 1: Shared Windows config path and migration

**Files:**
- Modify: `config.go` — split path selection from loading and add Windows ProgramData migration.
- Create: `config_windows.go` — Windows-only ProgramData path and executable-adjacent migration source.
- Create: `config_other.go` — non-Windows path behavior wrappers if needed to keep platform-specific code isolated.
- Test: `config_test.go` — path selection, migration, invalid source, and persistence behavior.

**Interfaces:**
- Produces `defaultConfigPath() string` with Windows returning `%ProgramData%\\patchbay\\portforward-config.json` and non-Windows retaining the executable-directory path.
- Produces `prepareConfigPath(path string) (string, error)` or an equivalent explicit helper used by `NewConfigStore`; tests must be able to inject a temporary destination/source instead of touching real `%ProgramData%`.
- Keeps `ConfigStore`, `NewConfigStore`, `Snapshot`, `AddRule`, `UpdateRule`, and `DeleteRule` public behavior unchanged.

- [ ] **Step 1: Write failing tests for platform path and migration decisions**

Add deterministic tests that use injected roots/helpers and assert:

```go
func TestConfigMigrationCopiesAdjacentConfigToDestination(t *testing.T) { /* source preserved, destination contents equal */ }
func TestConfigMigrationDoesNotOverwriteExistingDestination(t *testing.T) { /* existing destination wins */ }
func TestConfigMigrationRejectsInvalidSource(t *testing.T) { /* error, no destination overwrite */ }
func TestNewConfigStoreCreatesParentDirectory(t *testing.T) { /* nested destination is created */ }
```

Do not make Linux tests depend on actual Windows environment variables. Test the path/migration helper with explicit source and destination paths; cover platform wrappers separately where build tags permit.

- [ ] **Step 2: Run the focused config tests and verify they fail**

Run:

```bash
go test ./... -run 'Test(ConfigMigration|NewConfigStoreCreatesParentDirectory)' -count=1
```

Expected: FAIL because the migration/path helpers and the new parent-directory behavior do not exist.

- [ ] **Step 3: Implement the minimal path and migration behavior**

Keep JSON schema unchanged. Before first creation, create the destination parent directory. On Windows, when the ProgramData destination is absent and an adjacent executable config exists, copy the source bytes to the destination without deleting or modifying the source; validate JSON before writing. If the source is unreadable or invalid, return an error and do not create/replace the destination. If destination exists, load it directly and do not migrate.

- [ ] **Step 4: Run focused and existing config tests**

Run:

```bash
go test ./... -run 'Test(ConfigMigration|NewConfigStoreCreatesParentDirectory)' -count=1
go test ./... -run 'TestRoutes|TestManager' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the config boundary**

```bash
git add config.go config_windows.go config_other.go config_test.go
git commit -m "Use shared Windows config path"
```

---

### Task 2: Extract cancellable shared runtime

**Files:**
- Modify: `main.go` — replace monolithic startup with mode dispatch and shared runtime lifecycle.
- Create: `runtime.go` — runtime struct, construction, HTTP serving, enabled-rule startup, and idempotent shutdown.
- Modify: `proxy.go` only if the runtime needs an explicit context-aware `StopAll` boundary; preserve current connection cleanup.
- Test: `runtime_test.go` — enabled-rule startup selection and shutdown behavior using an injected HTTP listener/manager seam.

**Interfaces:**
- Produces `type runtimeApp struct { store *ConfigStore; manager *Manager; app *App; server *http.Server }` or equivalent focused runtime owner.
- Produces `newRuntime(store *ConfigStore, listenFn func(string, http.Handler) (net.Listener, error)) (*runtimeApp, error)` or an equivalent injectable constructor.
- Produces `start(ctx context.Context) error` and `stop() error`/`stop()` that start only `Rule.Enabled` rules, serve HTTP, and close listeners plus forwarding connections.
- `main()` dispatches `os.Args[1] == "service"` to the service entrypoint; ordinary mode retains tray behavior.

- [ ] **Step 1: Write failing tests for enabled startup and shutdown**

Add tests with a manager seam or local test listeners that prove:

```go
func TestRuntimeStartsOnlyEnabledRules(t *testing.T) { /* disabled rule never binds */ }
func TestRuntimeStopClosesHTTPAndForwardingRuntime(t *testing.T) { /* shutdown releases ports */ }
```

The tests must assert observable state (listener availability/manager state), not private implementation details.

- [ ] **Step 2: Run focused runtime tests and verify failure**

Run:

```bash
go test ./... -run 'TestRuntime' -count=1
```

Expected: FAIL because the shared runtime abstraction does not exist.

- [ ] **Step 3: Implement runtime extraction**

Move the current “load config → start enabled rules → create app → serve dashboard” sequence into the runtime owner. Use `http.Server` instead of `http.ListenAndServe` so shutdown can close the HTTP listener. Treat `http.ErrServerClosed` as normal. Do not call `os.Exit` from reusable runtime code. Preserve existing local mode behavior and the existing dashboard/manager interfaces.

- [ ] **Step 4: Run focused runtime tests**

Run:

```bash
go test ./... -run 'TestRuntime' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the shared runtime boundary**

```bash
git add main.go runtime.go runtime_test.go proxy.go
 git commit -m "Extract cancellable forwarding runtime"
```

---

### Task 3: Implement Windows Service Control Manager adapter

**Files:**
- Create: `service_windows.go` — service constants, SCM query, install/update, start, stop, delete, and bounded state waits.
- Create: `service_other.go` — non-Windows stubs returning a clear unsupported error or an uninstalled state.
- Test: `service_windows_test.go` or platform-neutral `service_test.go` — command-line construction and state transition helpers that do not require an actual Windows service.

**Interfaces:**
- Stable constants: `patchbayServiceName = "PatchbayPortForwarder"`, display name `patchbay port forwarder`.
- Produces `type serviceState int` with at least `serviceNotInstalled`, `serviceStopped`, `serviceRunning`.
- Produces `queryService() (serviceState, error)`.
- Produces `installService(executable string) error`, `startService() error`, `stopService() error`, `deleteService() error`.
- Produces `serviceCommandLine(executable string) string`, returning a correctly quoted executable path plus `service` argument for paths containing spaces.
- Uses `sc.exe` for management and polls/query state with a bounded deadline; no success is returned before the target state is observed.

- [ ] **Step 1: Write failing tests for service command construction and waits**

Add platform-neutral tests for:

```go
func TestServiceCommandLineQuotesExecutablePath(t *testing.T) {
    got := serviceCommandLine(`C:\Program Files\patchbay\patchbay.exe`)
    want := `"C:\Program Files\patchbay\patchbay.exe" service`
    if got != want { t.Fatalf("got %q, want %q", got, want) }
}
```

Also test that parser/state mapping distinguishes missing, stopped, and running results without invoking real SCM. Keep actual `sc.exe` calls behind an injectable command runner.

- [ ] **Step 2: Run focused service tests and verify failure**

Run:

```bash
go test ./... -run 'TestService' -count=1
```

Expected: FAIL because service constants, command construction, and state helpers do not exist.

- [ ] **Step 3: Implement the SCM adapter**

Use a small command-runner abstraction around `exec.Command("sc.exe", ...)` so tests can supply deterministic output. Implement:

```text
query:  sc.exe query PatchbayPortForwarder
create: sc.exe create PatchbayPortForwarder binPath= "<quoted exe> service" start= auto DisplayName= "patchbay port forwarder"
config: sc.exe description PatchbayPortForwarder "patchbay local dashboard and TCP/UDP forwarding service"
start:  sc.exe start PatchbayPortForwarder
stop:   sc.exe stop PatchbayPortForwarder
delete: sc.exe delete PatchbayPortForwarder
```

Handle “service does not exist” as `serviceNotInstalled`; surface all other command failures. Poll with a short bounded timeout and delay. Delete only after stopped.

- [ ] **Step 4: Run focused service tests**

Run:

```bash
go test ./... -run 'TestService' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the SCM adapter**

```bash
git add service_windows.go service_other.go service_windows_test.go service_test.go
git commit -m "Add Windows service control adapter"
```

---

### Task 4: Add Windows service entrypoint

**Files:**
- Create: `service_runtime_windows.go` — Windows service callback/dispatcher and service-owned runtime lifecycle.
- Create: `service_runtime_other.go` — non-Windows `runService() error` returning unsupported.
- Modify: `main.go` — dispatch `service` argument before tray setup.
- Test: `service_runtime_test.go` — test dispatch-independent runtime shutdown and service mode selection where possible.

**Interfaces:**
- Produces `runService() error`, Windows implementation registers with Service Control Manager and runs the shared runtime.
- Produces a service control handler that responds to stop/shutdown by canceling runtime and waiting for clean stop.
- The service entrypoint never creates tray UI, opens a browser, or calls `os.Exit` during normal stop.

- [ ] **Step 1: Write failing tests for service mode dispatch/runtime ownership**

Add tests proving the mode selector maps `service` to service mode and ordinary arguments to tray/local mode without starting both runtimes. Use an injected `runService`/`runTray` function if needed rather than launching SCM in tests.

- [ ] **Step 2: Run focused tests and verify failure**

Run:

```bash
go test ./... -run 'Test(ServiceMode|RunService)' -count=1
```

Expected: FAIL because dispatch and service runtime entrypoints do not exist.

- [ ] **Step 3: Implement the service dispatcher and callbacks**

On Windows, call `StartServiceCtrlDispatcherW` with the stable service name. In the service main callback, create the shared runtime, report `SERVICE_RUNNING`, wait for stop/shutdown, call runtime shutdown, then report `SERVICE_STOPPED`. Ensure service startup errors are reported to SCM and returned. On non-Windows, keep the build compiling with an unsupported stub.

- [ ] **Step 4: Run focused tests and Windows compile**

Run:

```bash
go test ./... -run 'Test(ServiceMode|RunService)' -count=1
task build-windows
```

Expected: PASS and a successful Windows GUI binary build.

- [ ] **Step 5: Commit the service entrypoint**

```bash
git add main.go service_runtime_windows.go service_runtime_other.go service_runtime_test.go
git commit -m "Run forwarding runtime as Windows service"
```

---

### Task 5: Convert tray into service-aware controller

**Files:**
- Modify: `main.go` — detect service installation/state before creating local runtime; keep tray UI-only when installed.
- Modify: `systray_windows.go` — menu items, checked/disabled state, service callbacks, and concise Windows error reporting.
- Modify: `systray_other.go` — preserve non-Windows development stub signature.
- Test: `tray_mode_test.go` — service-installed mode never creates a local manager; service-stopped mode exposes start action.

**Interfaces:**
- `runTray` receives callbacks for `openDashboard`, `queryService`, `enableService`, `startService`, `stopService`, `disableService`, and `quit`.
- `trayApp` stores current `serviceState` and callback functions; menu IDs include Open, Start, Stop, Enable, Disable, Quit.
- `Quit` stops local runtime only in local mode; in service-client mode it exits tray without stopping SCM service.
- Enable flow: install, set automatic start, start, wait running, then transition tray to service-client mode.
- Disable flow: stop, wait stopped, delete, then transition back to local mode only after all operations succeed.

- [ ] **Step 1: Write failing tests for mode ownership and state transitions**

Add deterministic tests that use fake service callbacks and assert:

```go
func TestInstalledServicePreventsLocalRuntimeStart(t *testing.T) { /* manager not started */ }
func TestEnableServiceTransitionsTrayToClient(t *testing.T) { /* install/start callbacks ordered */ }
func TestDisableFailureKeepsServiceClientMode(t *testing.T) { /* no local fallback after delete error */ }
```

- [ ] **Step 2: Run focused tray-mode tests and verify failure**

Run:

```bash
go test ./... -run 'Test(InstalledService|EnableService|DisableFailure)' -count=1
```

Expected: FAIL because service-aware mode selection and callbacks do not exist.

- [ ] **Step 3: Implement service-aware tray orchestration**

At startup, query SCM. If service is installed in any state, do not load/start a local forwarding runtime or bind the dashboard; create the tray as a client opening the service dashboard URL. If not installed, retain current local runtime/tray behavior. Render menu labels from current service state and refresh state after each operation. Surface failures through a Windows message box or a clear tray notification mechanism already available in the file; do not silently fall back.

- [ ] **Step 4: Run focused tray-mode tests and Windows compile**

Run:

```bash
go test ./... -run 'Test(InstalledService|EnableService|DisableFailure)' -count=1
task build-windows
```

Expected: PASS and successful Windows build.

- [ ] **Step 5: Commit tray integration**

```bash
git add main.go systray_windows.go systray_other.go tray_mode_test.go
 git commit -m "Add service mode controls to tray"
```

---

### Task 6: Update docs, Taskfile verification, and full smoke checks

**Files:**
- Modify: `README.md` — document service mode, shared config location, tray menu behavior, and Windows build/run commands.
- Modify: `docs/service-mode-design.md` only if implementation reveals a deliberate contract change; keep the spec authoritative.
- Modify: `Taskfile.yml` only if a service-specific build/run target is required; retain `build-windows` manifest/icon behavior.
- Test: existing test files only; add no duplicate tests unless a final uncovered contract remains.

**Interfaces:**
- User-facing commands remain `task test`, `task build`, and `task build-windows`.
- Windows binary remains GUI, requires administrator elevation, embeds manifest and icon, and supports `patchbay.exe service` for SCM.

- [ ] **Step 1: Update README with the implemented workflow**

Document:

```text
Double-click patchbay.exe -> tray mode.
Tray -> Enable service mode -> UAC -> service AUTO_START.
Service owns forwarding/dashboard after reboot.
Tray Quit does not stop an installed service.
Rule Enabled values persist in %ProgramData%\\patchbay\\portforward-config.json.
```

Include the distinction between `Stop service` and stopping individual rules.

- [ ] **Step 2: Run full verification**

Run:

```bash
task test
go test -race ./...
task build
task build-windows
git diff --check
git status --short --untracked-files=all
```

Expected: all tests/builds pass, no whitespace errors, and only intentional generated artifacts are ignored.

- [ ] **Step 3: Perform Windows-host manual verification**

On Windows, verify:

1. `task build-windows` binary starts without a console window and shows the tray.
2. `Enable service mode` prompts UAC, creates `PatchbayPortForwarder`, sets Automatic startup, and starts it.
3. The service owns port 8787 and enabled rules; the tray does not bind duplicate ports.
4. Stopping/starting individual rules changes `Rule.Enabled` in the ProgramData config.
5. Tray `Quit` leaves the service and forwarding alive.
6. Reboot starts the service before login.
7. `Disable service mode` stops/deletes the service and only then returns to local mode.

- [ ] **Step 4: Commit final docs/verification changes**

```bash
git add README.md docs/service-mode-design.md Taskfile.yml
 git commit -m "Document Windows service mode usage"
```

---

## Self-review checklist

- Spec coverage: config migration is Task 1; shared runtime and clean stop are Task 2; SCM commands/state waits are Task 3; service dispatcher is Task 4; tray ownership/menu transitions are Task 5; documentation and all required verification are Task 6.
- Placeholder scan: no TBD/TODO or unspecified implementation steps; all commands, paths, names, and expected outcomes are concrete.
- Interface consistency: `runService`, `queryService`, `installService`, `startService`, `stopService`, `deleteService`, `serviceCommandLine`, and `runtimeApp` are introduced before consumers; Task 5 consumes the SCM and runtime boundaries from Tasks 2–4.
- Scope boundary: no authentication, named pipe, third-party service package, or unrelated dashboard redesign is included.
- Risk called out: localhost rule API remains unauthenticated by explicit user decision; service ownership prevents duplicate listeners; service stop is cancellable and bounded.
