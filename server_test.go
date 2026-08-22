package main

import (
	"io"
	"net/http"
	"net/http/httptest"
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
