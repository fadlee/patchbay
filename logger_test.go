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
