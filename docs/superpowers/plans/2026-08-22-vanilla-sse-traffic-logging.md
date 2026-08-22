# Vanilla JS, SSE Realtime Dashboard, & Traffic Logging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the vendored HTMX library and polling-based UI with native Vanilla JS and Server-Sent Events (SSE). Add an integrated traffic logging subsystem with an in-memory ring buffer for live streaming, file-based persistence for historical traffic analysis, and native Canvas-based visual bandwidth/connection charts.

**Architecture:** Build a Go standard-library-only realtime backend (`net/http` + SSE broadcaster) and in-memory ring buffer coupled with daily JSON Lines log files (`traffic-YYYY-MM-DD.jsonl`). Refactor proxy connection handlers to record connection sessions (IPs, protocol, bytes in/out, duration). Replace HTMX frontend with single-file lightweight Vanilla JS (`app.js`) utilizing native `EventSource`, standard `fetch()`, and HTML5 `<canvas>` charting.

**Tech Stack:** Go 1.22+ stdlib (`net/http`, `sync`, `sync/atomic`, `encoding/json`, `os`, `time`), Vanilla JavaScript (ES6+), HTML5 Canvas, Taskfile.

**Spec:** `docs/superpowers/specs/2026-08-22-vanilla-sse-traffic-logging-design.md`

---

## Global Constraints

- Zero external vendor dependencies: remove `web/static/htmx.min.js`; no npm, no charting libraries, no WebSocket libraries.
- Standard library Go only: SSE and REST endpoints must use standard `net/http`.
- Log file directory: `%ProgramData%\patchbay\logs\` on Windows, `./logs/` on non-Windows.
- In-memory ring buffer default capacity: 1,000 entries.
- Realtime SSE stream on `GET /api/events` emitting `event: stats` and `event: log`.
- TDD required: write failing unit/integration tests before implementing each backend capability.
- Full verification at the end: `task test`, `go test -race ./...`, `task build`, and `task build-windows`.

---

### Task 1: Traffic Logger Engine & In-Memory Ring Buffer

**Files:**
- Create: `logger.go` — log event structure, ring buffer with thread-safe append/query, and daily rotating file writer.
- Create: `logger_other.go` — default log directory for non-Windows (`./logs`).
- Create: `logger_windows.go` — default log directory for Windows (`%ProgramData%\patchbay\logs`).
- Test: `logger_test.go` — test ring buffer capacity/eviction, query filtering, and JSON Lines file persistence.

**Interfaces:**
- Produces:
  ```go
  type LogEntry struct {
      ID         string    `json:"id"`
      Time       time.Time `json:"time"`
      RuleID     string    `json:"rule_id"`
      RuleName   string    `json:"rule_name"`
      Protocol   string    `json:"protocol"`
      ClientAddr string    `json:"client_addr"`
      TargetAddr string    `json:"target_addr"`
      BytesIn    int64     `json:"bytes_in"`
      BytesOut   int64     `json:"bytes_out"`
      DurationMS int64     `json:"duration_ms"`
      Status     string    `json:"status"` // "active", "closed", "error"
  }

  type TrafficLogger struct { ... }
  func NewTrafficLogger(dir string, capacity int) (*TrafficLogger, error)
  func (l *TrafficLogger) Record(entry LogEntry)
  func (l *TrafficLogger) Recent(limit int, ruleID string) []LogEntry
  func (l *TrafficLogger) Clear()
  func (l *TrafficLogger) Close() error
  ```

- [ ] **Step 1: Write failing tests for ring buffer & file logging**

```go
// logger_test.go
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRingBufferCapacityAndEviction(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewTrafficLogger(dir, 3)
	if err != nil {
		t.Fatalf("NewTrafficLogger: %v", err)
	}
	defer logger.Close()

	for i := 1; i <= 5; i++ {
		logger.Record(LogEntry{
			ID:       itoa(i),
			Time:     time.Now(),
			RuleID:   "r1",
			RuleName: "Rule 1",
			Status:   "closed",
		})
	}

	entries := logger.Recent(10, "")
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].ID != "5" || entries[2].ID != "3" {
		t.Fatalf("expected newest to oldest (5,4,3), got (%s,%s,%s)", entries[0].ID, entries[1].ID, entries[2].ID)
	}
}

func TestRingBufferFilterByRuleID(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewTrafficLogger(dir, 10)
	if err != nil {
		t.Fatalf("NewTrafficLogger: %v", err)
	}
	defer logger.Close()

	logger.Record(LogEntry{ID: "1", RuleID: "rule-a", Status: "closed"})
	logger.Record(LogEntry{ID: "2", RuleID: "rule-b", Status: "closed"})
	logger.Record(LogEntry{ID: "3", RuleID: "rule-a", Status: "closed"})

	aEntries := logger.Recent(10, "rule-a")
	if len(aEntries) != 2 {
		t.Fatalf("expected 2 rule-a entries, got %d", len(aEntries))
	}
}

func TestPersistentLogFileAppend(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewTrafficLogger(dir, 10)
	if err != nil {
		t.Fatalf("NewTrafficLogger: %v", err)
	}

	entry := LogEntry{
		ID:         "test-id",
		Time:       time.Date(2026, 8, 22, 20, 0, 0, 0, time.UTC),
		RuleID:     "rule-1",
		RuleName:   "Web",
		Protocol:   "tcp",
		ClientAddr: "127.0.0.1:50000",
		TargetAddr: "127.0.0.1:8080",
		BytesIn:    100,
		BytesOut:   200,
		DurationMS: 50,
		Status:     "closed",
	}
	logger.Record(entry)
	logger.Close()

	expectedFile := filepath.Join(dir, "traffic-2026-08-22.jsonl")
	data, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty log file")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./... -run 'Test(RingBuffer|PersistentLogFile)' -count=1`
Expected: FAIL due to undeclared `TrafficLogger` and `LogEntry`.

- [ ] **Step 3: Implement minimal TrafficLogger and platform log directory**

Create `logger.go`, `logger_windows.go`, and `logger_other.go` with mutex-protected slice ring buffer, filter method, and daily JSONL append file writer.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -run 'Test(RingBuffer|PersistentLogFile)' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add logger.go logger_windows.go logger_other.go logger_test.go
git commit -m "Implement traffic logger engine with in-memory ring buffer and file persistence"
```

---

### Task 2: Proxy Connection Session Tracking & Logger Integration

**Files:**
- Modify: `proxy.go` — integrate `TrafficLogger` into `Manager`, track TCP & UDP session lifetimes, count bytes, and record completed sessions to logger.
- Test: `proxy_test.go` — add test verifying connection records are pushed to logger on client disconnect.

**Interfaces:**
- Consumes: `TrafficLogger.Record(LogEntry)` from Task 1.
- Produces:
  ```go
  func NewManagerWithLogger(logger *TrafficLogger) *Manager
  func (m *Manager) SetLogger(logger *TrafficLogger)
  ```

- [ ] **Step 1: Write failing test in proxy_test.go**

```go
func TestManagerLogsConnectionSessionOnClose(t *testing.T) {
	logDir := t.TempDir()
	logger, err := NewTrafficLogger(logDir, 100)
	if err != nil {
		t.Fatalf("NewTrafficLogger: %v", err)
	}
	defer logger.Close()

	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen target: %v", err)
	}
	defer target.Close()

	go func() {
		for {
			conn, err := target.Accept()
			if err != nil {
				return
			}
			_, _ = io.WriteString(conn, "pong")
			conn.Close()
		}
	}()

	listenPort := freeTCPPort(t)
	manager := NewManagerWithLogger(logger)
	rule := Rule{
		ID:         "log-test",
		Name:       "Log Test",
		Protocol:   "tcp",
		ListenAddr: "127.0.0.1",
		ListenPort: listenPort,
		TargetAddr: "127.0.0.1",
		TargetPort: target.Addr().(*net.TCPAddr).Port,
	}
	if err := manager.Start(rule); err != nil {
		t.Fatalf("start rule: %v", err)
	}
	defer manager.Stop(rule.ID)

	client, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", itoa(listenPort)))
	if err != nil {
		t.Fatalf("dial rule: %v", err)
	}
	_, _ = io.ReadAll(client)
	client.Close()

	// Wait briefly for goroutine to finish and record log
	time.Sleep(50 * time.Millisecond)

	recent := logger.Recent(10, rule.ID)
	if len(recent) == 0 {
		t.Fatal("expected at least 1 log entry recorded for connection session")
	}
	if recent[0].RuleID != "log-test" || recent[0].BytesOut < 4 {
		t.Fatalf("unexpected log entry: %+v", recent[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestManagerLogsConnectionSessionOnClose -count=1`
Expected: FAIL due to undeclared `NewManagerWithLogger`.

- [ ] **Step 3: Modify proxy.go to track connection sessions**

In `proxy.go`:
- Store optional `*TrafficLogger` in `Manager`.
- In `startTCP`, wrap client & target conns with counting readers/writers, record start time, generate session ID, and emit `LogEntry` with status `closed` or `error` when connection completes.
- In `startUDP`, record session log entry upon UDP session expiry.

- [ ] **Step 4: Run proxy tests to verify they pass**

Run: `go test ./... -run 'TestManager' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add proxy.go proxy_test.go
git commit -m "Integrate connection session logging into proxy manager"
```

---

### Task 3: SSE Broadcaster & REST Endpoints

**Files:**
- Create: `sse.go` — SSE broadcaster hub supporting multiple clients, heartbeats, and typed events (`stats`, `log`).
- Modify: `server.go` — replace HTMX endpoints with REST JSON endpoints (`GET /api/rules`, `POST /api/rules`, `PUT /api/rules/{id}`, `DELETE /api/rules/{id}`, `POST /api/rules/{id}/toggle`, `GET /api/logs`, `POST /api/logs/clear`) and `GET /api/events`.
- Test: `server_test.go` — test SSE event streaming and REST JSON CRUD responses.

**Interfaces:**
- Consumes: `TrafficLogger`, `Manager`, `ConfigStore`.
- Produces:
  ```go
  type SSEHub struct { ... }
  func NewSSEHub() *SSEHub
  func (h *SSEHub) Broadcast(event string, data any)
  func (h *SSEHub) Handler() http.HandlerFunc
  ```

- [ ] **Step 1: Write failing tests for SSE and JSON REST APIs**

```go
// server_test.go updates
func TestSSEStreamEmitsStatsAndHeartbeat(t *testing.T) {
	hub := NewSSEHub()
	defer hub.Close()

	srv := httptest.NewServer(hub.Handler())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("expected Content-Type text/event-stream, got %s", resp.Header.Get("Content-Type"))
	}
}

func TestRulesJSONEndpoints(t *testing.T) {
	store, _ := NewConfigStore(t.TempDir() + "/config.json")
	manager := NewManager()
	logger, _ := NewTrafficLogger(t.TempDir(), 100)
	app := NewApp(store, manager, logger)

	// Test GET /api/rules
	req := httptest.NewRequest(http.MethodGet, "/api/rules", nil)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/rules status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("expected application/json, got %s", rec.Header().Get("Content-Type"))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./... -run 'Test(SSEStream|RulesJSONEndpoints)' -count=1`
Expected: FAIL due to undeclared `NewSSEHub` and updated `NewApp`.

- [ ] **Step 3: Implement SSEHub and REST endpoints in Go**

- In `sse.go`, build client registration channel, broadcast channel, and SSE stream formatting `event: <name>\ndata: <json>\n\n`.
- In `server.go`, add JSON helpers, REST route handlers under `/api/`, register `GET /api/events`, and update `runtime.go` to pipe periodic stats and live log emissions to `SSEHub`.

- [ ] **Step 4: Run server tests to verify they pass**

Run: `go test ./... -run 'Test(SSEStream|RulesJSONEndpoints|Routes)' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sse.go server.go server_test.go runtime.go
git commit -m "Implement SSE broadcaster and JSON REST endpoints"
```

---

### Task 4: Traffic Summary & Aggregation Engine

**Files:**
- Modify: `logger.go` — add aggregation logic reading recent history / daily log file to compute bandwidth per hour/minute and total volume per rule.
- Modify: `server.go` — expose `GET /api/logs/summary`.
- Test: `logger_test.go` — test calculation of traffic metrics by hour bucket.

**Interfaces:**
- Produces:
  ```go
  type TrafficBucket struct {
      Timestamp  time.Time `json:"timestamp"`
      BytesIn    int64     `json:"bytes_in"`
      BytesOut   int64     `json:"bytes_out"`
      Connection int       `json:"connections"`
  }
  type TrafficSummary struct {
      Buckets []TrafficBucket `json:"buckets"`
      TotalIn int64           `json:"total_in"`
      TotalOut int64          `json:"total_out"`
      TotalConns int          `json:"total_conns"`
  }
  func (l *TrafficLogger) Summary(since time.Time) TrafficSummary
  ```

- [ ] **Step 1: Write failing test in logger_test.go**

```go
func TestTrafficSummaryAggregation(t *testing.T) {
	dir := t.TempDir()
	logger, _ := NewTrafficLogger(dir, 100)
	defer logger.Close()

	now := time.Now().Truncate(time.Hour)
	logger.Record(LogEntry{Time: now.Add(5 * time.Minute), BytesIn: 1000, BytesOut: 2000, Status: "closed"})
	logger.Record(LogEntry{Time: now.Add(15 * time.Minute), BytesIn: 500, BytesOut: 1500, Status: "closed"})

	summary := logger.Summary(now)
	if summary.TotalIn != 1500 || summary.TotalOut != 3500 || summary.TotalConns != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestTrafficSummaryAggregation -count=1`
Expected: FAIL due to undeclared `Summary`.

- [ ] **Step 3: Implement Summary aggregation method and wire endpoint**

- Implement `Summary(since time.Time)` in `logger.go` aggregating in-memory ring buffer (and fallback reading today's `.jsonl` if needed).
- Wire `GET /api/logs/summary` in `server.go`.

- [ ] **Step 4: Run logger tests to verify they pass**

Run: `go test ./... -run 'TestTrafficSummary' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add logger.go server.go logger_test.go server_test.go
git commit -m "Add traffic aggregation summary engine and endpoint"
```

---

### Task 5: Frontend Migration to Vanilla JS, Canvas Chart & SSE

**Files:**
- Remove: `web/static/htmx.min.js`
- Create: `web/static/app.js` — SSE client, dynamic rule table renderer, REST API CRUD calls, modal logic, log stream viewer, and HTML5 Canvas bandwidth chart.
- Modify: `web/static/style.css` — styles for realtime status badge, live log table, and chart container.
- Modify: `web/templates/index.html` — remove HTMX script tag, clean semantic markup with IDs and templates.
- Remove: `web/templates/rules_table.html` (no longer needed since table is rendered dynamically by Vanilla JS).
- Test: `server_test.go` — verify static assets serve `app.js` and `style.css` without `htmx.min.js`.

**Interfaces:**
- Frontend uses:
  - `const es = new EventSource('/api/events');`
  - `fetch('/api/rules')`, `fetch('/api/logs')`, `fetch('/api/logs/summary')`
  - Canvas 2D context (`ctx.beginPath()`, `ctx.lineTo()`, `ctx.stroke()`) for responsive smooth chart rendering.

- [ ] **Step 1: Write failing test verifying embedded asset changes**

```go
func TestStaticAssetsServeVanillaAppJS(t *testing.T) {
	store, _ := NewConfigStore(t.TempDir() + "/config.json")
	manager := NewManager()
	logger, _ := NewTrafficLogger(t.TempDir(), 100)
	app := NewApp(store, manager, logger)

	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /static/app.js status = %d, want 200", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestStaticAssetsServeVanillaAppJS -count=1`
Expected: FAIL with 404 Not Found.

- [ ] **Step 3: Implement Vanilla JS app, Canvas Chart, updated CSS, and HTML template**

1. Create `web/static/app.js`:
   - Initialize `EventSource('/api/events')` with reconnect status indicators.
   - Listen to `stats` event: update rule table numbers and push data points to the Canvas chart.
   - Listen to `log` event: prepend live connection rows to the log table.
   - Implement Add/Edit/Delete/Toggle rule actions via `fetch()`.
   - Implement Canvas line/area chart renderer with auto-scaling Y axis and responsive resize.
2. Update `web/templates/index.html` and `web/static/style.css`.
3. Delete `web/static/htmx.min.js` and `web/templates/rules_table.html`.

- [ ] **Step 4: Run server tests to verify static asset serving and rendering**

Run: `go test ./... -run 'TestStaticAssets|TestRoutes' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git rm web/static/htmx.min.js web/templates/rules_table.html
git add web/static/app.js web/static/style.css web/templates/index.html server.go server_test.go
git commit -m "Migrate dashboard UI from HTMX to native Vanilla JS, Canvas chart, and SSE"
```

---

### Task 6: Full Verification, Documentation & Build Checks

**Files:**
- Modify: `README.md` — document Vanilla JS architecture, SSE stream, traffic logging locations, and REST API.
- Test: All repository unit & race tests.

- [ ] **Step 1: Update README.md**

Update README feature list, architecture overview, and project layout table reflecting Vanilla JS, SSE, and traffic logging files.

- [ ] **Step 2: Run full test suite with race detector**

Run:
```bash
task test
go test -race ./...
```
Expected: PASS with 0 failures and 0 race warnings.

- [ ] **Step 3: Run cross-platform binary builds**

Run:
```bash
task build
task build-windows
```
Expected: PASS, producing `dist/patchbay` and `dist/patchbay-windows-amd64.exe`.

- [ ] **Step 4: Check git status and whitespace**

Run:
```bash
git diff --check
git status --short
```
Expected: 0 whitespace errors, clean untracked state.

- [ ] **Step 5: Commit documentation and final cleanups**

```bash
git add README.md
git commit -m "Document Vanilla JS, SSE realtime dashboard, and traffic logging"
```

---

## Self-Review

- **Spec coverage:** Ring buffer and JSON Lines file logging covered in Task 1 & 2; SSE broadcaster and REST APIs covered in Task 3; Traffic aggregation summary in Task 4; Vanilla JS, Canvas charting, and HTMX removal in Task 5; Verification & README update in Task 6.
- **Placeholder scan:** No "TBD", "TODO", or vague requirements. All function signatures and test structures are concrete.
- **Type consistency:** `LogEntry`, `TrafficLogger`, `SSEHub`, and `TrafficSummary` types remain identical across all tasks.
- **Zero-dependency constraint:** `htmx.min.js` deleted; Go standard library used exclusively.
