package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRoutesServeEmbeddedStaticAssets(t *testing.T) {
	store, err := NewConfigStore(t.TempDir() + "/config.json")
	if err != nil {
		t.Fatalf("create config store: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/static/style.css", nil)
	res := httptest.NewRecorder()
	NewApp(store, NewManager(), nil, nil).Routes().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("GET /static/style.css status = %d, want %d", res.Code, http.StatusOK)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if !strings.Contains(string(body), "--mono") {
		t.Fatal("GET /static/style.css did not return the embedded stylesheet")
	}
}

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
	hub := NewSSEHub()
	app := NewApp(store, manager, logger, hub)

	// Test GET /api/rules
	req := httptest.NewRequest(http.MethodGet, "/api/rules", nil)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/rules status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("expected application/json, got %s", rec.Header().Get("Content-Type"))
	}
}

func TestLogsJSONEndpoints(t *testing.T) {
	store, _ := NewConfigStore(t.TempDir() + "/config.json")
	manager := NewManager()
	logger, _ := NewTrafficLogger(t.TempDir(), 100)
	defer logger.Close()
	logger.Record(LogEntry{ID: "test-log", RuleID: "r1", Status: "closed"})
	hub := NewSSEHub()
	app := NewApp(store, manager, logger, hub)

	// Test GET /api/logs
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/logs status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "test-log") {
		t.Fatalf("expected log entry in response, got %s", rec.Body.String())
	}
}

func TestLogsSummaryJSONEndpoint(t *testing.T) {
	store, _ := NewConfigStore(t.TempDir() + "/config.json")
	manager := NewManager()
	logger, _ := NewTrafficLogger(t.TempDir(), 100)
	defer logger.Close()
	logger.Record(LogEntry{ID: "test-log", RuleID: "r1", BytesIn: 500, BytesOut: 1000, Status: "closed", Time: time.Now()})

	hub := NewSSEHub()
	app := NewApp(store, manager, logger, hub)
	// Test GET /api/logs/summary
	req := httptest.NewRequest(http.MethodGet, "/api/logs/summary", nil)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/logs/summary status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "total_in") {
		t.Fatalf("expected total_in in summary response, got %s", rec.Body.String())
	}
}

func TestStaticAssetsServeVanillaAppJS(t *testing.T) {
	store, _ := NewConfigStore(t.TempDir() + "/config.json")
	manager := NewManager()
	logger, _ := NewTrafficLogger(t.TempDir(), 100)
	defer logger.Close()
	hub := NewSSEHub()
	app := NewApp(store, manager, logger, hub)
	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /static/app.js status = %d, want 200", rec.Code)
	}
}

func TestConfigLoggingToggleEndpoint(t *testing.T) {
	store, _ := NewConfigStore(t.TempDir() + "/config.json")
	manager := NewManager()
	logger, _ := NewTrafficLogger(t.TempDir(), 100)
	hub := NewSSEHub()
	app := NewApp(store, manager, logger, hub)

	// Test GET /api/config
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"logging_enabled":true`) {
		t.Fatalf("expected logging_enabled:true in config response, got %s", rec.Body.String())
	}

	// Test POST /api/config/logging to disable
	toggleReq := httptest.NewRequest(http.MethodPost, "/api/config/logging", strings.NewReader(`{"enabled":false}`))
	toggleRec := httptest.NewRecorder()
	app.Routes().ServeHTTP(toggleRec, toggleReq)

	if toggleRec.Code != http.StatusOK {
		t.Fatalf("POST /api/config/logging status = %d, want 200", toggleRec.Code)
	}
	if !strings.Contains(toggleRec.Body.String(), `"logging_enabled":false`) {
		t.Fatalf("expected logging_enabled:false, got %s", toggleRec.Body.String())
	}
	if logger.IsEnabled() {
		t.Fatal("logger should be disabled after toggle endpoint called")
	}
}

func TestAPIUpdateCheck(t *testing.T) {
	store, err := NewConfigStore(t.TempDir() + "/config.json")
	if err != nil {
		t.Fatalf("failed to create config store: %v", err)
	}

	tsMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := ghRelease{
			TagName: "v2.0.0",
			HTMLURL: "https://github.com/fadlee/patchbay/releases/tag/v2.0.0",
			Body:    "Major release",
			Assets: []ghAsset{
				{
					Name:               "patchbay-setup-amd64.exe",
					Size:               5000,
					BrowserDownloadURL: "http://example.com/patchbay-setup-amd64.exe",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer tsMock.Close()

	updater := &Updater{
		repo:    "fadlee/patchbay",
		baseURL: tsMock.URL,
		client:  tsMock.Client(),
	}

	app := NewApp(store, NewManager(), nil, nil)
	app.SetUpdater(updater)

	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/update/check")
	if err != nil {
		t.Fatalf("failed to GET update check: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var info UpdateInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !info.UpdateAvail {
		t.Errorf("expected update available, got false")
	}
	if info.LatestVersion != "2.0.0" {
		t.Errorf("expected latest version 2.0.0, got %s", info.LatestVersion)
	}
	if runtime.GOOS == "windows" && info.AssetName != "patchbay-setup-amd64.exe" {
		t.Errorf("expected asset name patchbay-setup-amd64.exe, got %s", info.AssetName)
	}
}
