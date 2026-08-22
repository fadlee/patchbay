package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogEntry records metadata for a completed or active connection session.
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

// TrafficLogger holds an in-memory ring buffer for low-latency live inspection
// and streams entries to rotating daily JSON Lines files on disk.
type TrafficLogger struct {
	mu          sync.RWMutex
	dir         string
	capacity    int
	enabled     bool
	entries     []LogEntry
	currentDay  string
	currentFile *os.File
	onRecord    func(LogEntry)
}

// NewTrafficLogger initializes the logger with the specified directory and
// ring buffer capacity.
func NewTrafficLogger(dir string, capacity int) (*TrafficLogger, error) {
	if dir == "" {
		dir = defaultLogDir()
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	if capacity <= 0 {
		capacity = 1000
	}
	return &TrafficLogger{
		dir:      dir,
		capacity: capacity,
		enabled:  true,
		entries:  make([]LogEntry, 0, capacity),
	}, nil
}

// SetOnRecord assigns an optional hook invoked whenever a new log entry is recorded
// (e.g. for pushing live events to an SSE stream).
func (l *TrafficLogger) SetOnRecord(fn func(LogEntry)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onRecord = fn
}

// SetEnabled turns recording on or off globally.
func (l *TrafficLogger) SetEnabled(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = enabled
}

// IsEnabled reports whether the logger is actively recording sessions.
func (l *TrafficLogger) IsEnabled() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.enabled
}

// Record appends an entry to the ring buffer and writes it to the daily log file.
func (l *TrafficLogger) Record(entry LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.enabled {
		return
	}
	// Append to in-memory ring buffer (newest at the end)
	if len(l.entries) >= l.capacity {
		copy(l.entries, l.entries[1:])
		l.entries[len(l.entries)-1] = entry
	} else {
		l.entries = append(l.entries, entry)
	}

	// Persist to daily jsonl file
	l.appendFileLocked(entry)

	if l.onRecord != nil {
		l.onRecord(entry)
	}
}

// Recent returns up to limit entries ordered from newest to oldest.
// If ruleID is non-empty, only entries matching that rule are returned.
func (l *TrafficLogger) Recent(limit int, ruleID string) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if limit <= 0 || limit > len(l.entries) {
		limit = len(l.entries)
	}

	res := make([]LogEntry, 0, limit)
	// Iterate in reverse (newest first)
	for i := len(l.entries) - 1; i >= 0 && len(res) < limit; i-- {
		e := l.entries[i]
		if ruleID == "" || e.RuleID == ruleID {
			res = append(res, e)
		}
	}
	return res
}

// Clear empties the in-memory ring buffer.
func (l *TrafficLogger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = l.entries[:0]
}

func (l *TrafficLogger) appendFileLocked(entry LogEntry) {
	if l.dir == "" {
		return
	}
	day := entry.Time.Format("2006-01-02")
	if l.currentFile == nil || l.currentDay != day {
		if l.currentFile != nil {
			_ = l.currentFile.Close()
		}
		path := filepath.Join(l.dir, fmt.Sprintf("traffic-%s.jsonl", day))
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return
		}
		l.currentFile = f
		l.currentDay = day
	}

	data, err := json.Marshal(entry)
	if err == nil {
		_, _ = l.currentFile.Write(append(data, '\n'))
	}
}

// Close closes the currently open log file handle.
func (l *TrafficLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.currentFile != nil {
		err := l.currentFile.Close()
		l.currentFile = nil
		return err
	}
	return nil
}

// TrafficBucket aggregates bytes and connections within a time slice.
type TrafficBucket struct {
	Timestamp   time.Time `json:"timestamp"`
	BytesIn     int64     `json:"bytes_in"`
	BytesOut    int64     `json:"bytes_out"`
	Connections int       `json:"connections"`
}

// TrafficSummary provides aggregate totals and time-series buckets for charting.
type TrafficSummary struct {
	Buckets    []TrafficBucket `json:"buckets"`
	TotalIn    int64           `json:"total_in"`
	TotalOut   int64           `json:"total_out"`
	TotalConns int             `json:"total_conns"`
}

// Summary aggregates traffic events recorded since the specified timestamp.
func (l *TrafficLogger) Summary(since time.Time) TrafficSummary {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var sum TrafficSummary
	bucketMap := make(map[int64]*TrafficBucket)

	for _, e := range l.entries {
		if !since.IsZero() && e.Time.Before(since) {
			continue
		}
		sum.TotalIn += e.BytesIn
		sum.TotalOut += e.BytesOut
		sum.TotalConns++

		// Group into 5-minute buckets for chart points
		bucketTime := e.Time.Truncate(5 * time.Minute)
		key := bucketTime.Unix()
		b, ok := bucketMap[key]
		if !ok {
			b = &TrafficBucket{Timestamp: bucketTime}
			bucketMap[key] = b
		}
		b.BytesIn += e.BytesIn
		b.BytesOut += e.BytesOut
		b.Connections++
	}

	// Convert map to slice
	for _, b := range bucketMap {
		sum.Buckets = append(sum.Buckets, *b)
	}
	return sum
}
