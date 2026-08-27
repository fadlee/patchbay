package patchbay

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)
// Embed web assets
//go:embed web/templates/*.html
var templateFS embed.FS
//go:embed web/static/*
var staticFS embed.FS

var tmpl = template.Must(template.ParseFS(templateFS, "web/templates/*.html"))

// Rule represents a single port forwarding rule.
type Rule struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`    // "tcp", "udp", or "tcp+udp"
	ListenAddr string `json:"listen_addr"` // e.g. "0.0.0.0"
	ListenPort int    `json:"listen_port"`
	TargetAddr string `json:"target_addr"`
	TargetPort int    `json:"target_port"`
	Enabled    bool   `json:"enabled"`
}

// Config is the full persisted application state.
type Config struct {
	AdminPort      int    `json:"admin_port"`
	LoggingEnabled *bool  `json:"logging_enabled,omitempty"`
	Rules          []Rule `json:"rules"`
}

func (c Config) IsLoggingEnabled() bool {
	if c.LoggingEnabled == nil {
		return true
	}
	return *c.LoggingEnabled
}

const ConfigFileName = "portforward-config.json"

// ConfigStore handles thread-safe loading/saving of Config to disk.
type ConfigStore struct {
	mu   sync.Mutex
	path string
	cfg  Config
}

func NewConfigStore(path string) (*ConfigStore, error) {
	if path == "" {
		path = ConfigFileName
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}

	cs := &ConfigStore{path: path}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		defLog := true
		cs.cfg = Config{AdminPort: 8787, LoggingEnabled: &defLog, Rules: []Rule{}}
		if saveErr := cs.saveLocked(); saveErr != nil {
			return nil, saveErr
		}
		return cs, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.AdminPort == 0 {
		cfg.AdminPort = 8787
	}
	cs.cfg = cfg
	return cs, nil
}

func (cs *ConfigStore) saveLocked() error {
	data, err := json.MarshalIndent(cs.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := cs.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, cs.path)
}

func (cs *ConfigStore) Snapshot() Config {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cp := cs.cfg
	cp.Rules = append([]Rule{}, cs.cfg.Rules...)
	return cp
}

func (cs *ConfigStore) AddRule(r Rule) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.cfg.Rules = append(cs.cfg.Rules, r)
	return cs.saveLocked()
}

func (cs *ConfigStore) UpdateRule(id string, mutate func(*Rule)) (Rule, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for i := range cs.cfg.Rules {
		if cs.cfg.Rules[i].ID == id {
			mutate(&cs.cfg.Rules[i])
			if err := cs.saveLocked(); err != nil {
				return Rule{}, err
			}
			return cs.cfg.Rules[i], nil
		}
	}
	return Rule{}, fmt.Errorf("rule not found: %s", id)
}

func (cs *ConfigStore) DeleteRule(id string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for i, r := range cs.cfg.Rules {
		if r.ID == id {
			cs.cfg.Rules = append(cs.cfg.Rules, cs.cfg.Rules[i+1:]...)
			return cs.saveLocked()
		}
	}
	return fmt.Errorf("rule not found: %s", id)
}

func (cs *ConfigStore) SetLoggingEnabled(enabled bool) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.cfg.LoggingEnabled = &enabled
	return cs.saveLocked()
}

func (cs *ConfigStore) SetAdminPort(port int) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.cfg.AdminPort = port
	return cs.saveLocked()
}

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
	Status     string    `json:"status"`
}

// TrafficLogger holds an in-memory ring buffer and rotating logs.
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

func NewTrafficLogger(dir string, capacity int) (*TrafficLogger, error) {
	if dir == "" {
		dir = "logs"
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

func (l *TrafficLogger) SetOnRecord(fn func(LogEntry)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onRecord = fn
}

func (l *TrafficLogger) SetEnabled(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = enabled
}

func (l *TrafficLogger) IsEnabled() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.enabled
}

func (l *TrafficLogger) Record(entry LogEntry) {
	l.mu.Lock()
	if !l.enabled {
		l.mu.Unlock()
		return
	}
	if len(l.entries) >= l.capacity {
		l.entries = l.entries[1:]
	}
	l.entries = append(l.entries, entry)
	cb := l.onRecord
	l.mu.Unlock()

	if cb != nil {
		cb(entry)
	}
}

func (l *TrafficLogger) RecentEntries(limit int) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if limit <= 0 || limit > len(l.entries) {
		limit = len(l.entries)
	}
	start := len(l.entries) - limit
	res := make([]LogEntry, limit)
	copy(res, l.entries[start:])
	return res
}

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

// Stats holds live counters for a running rule.
type Stats struct {
	ActiveConns int64 `json:"active_conns"`
	TotalConns  int64 `json:"total_conns"`
	BytesIn     int64 `json:"bytes_in"`
	BytesOut    int64 `json:"bytes_out"`
}

type runningRule struct {
	cancel context.CancelFunc
	stats  *Stats
	conns  *connTracker
}

type connTracker struct {
	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

func newConnTracker() *connTracker {
	return &connTracker{conns: make(map[net.Conn]struct{})}
}

func (t *connTracker) add(conn net.Conn) {
	t.mu.Lock()
	t.conns[conn] = struct{}{}
	t.mu.Unlock()
}

func (t *connTracker) remove(conn net.Conn) {
	t.mu.Lock()
	delete(t.conns, conn)
	t.mu.Unlock()
}

func (t *connTracker) closeAll() {
	t.mu.Lock()
	conns := make([]net.Conn, 0, len(t.conns))
	for conn := range t.conns {
		conns = append(conns, conn)
	}
	t.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

// Manager manages running forwarding rules.
type Manager struct {
	mu      sync.RWMutex
	running map[string]*runningRule
	logger  *TrafficLogger
}

func NewManager() *Manager {
	return &Manager{
		running: make(map[string]*runningRule),
	}
}

func (m *Manager) SetLogger(l *TrafficLogger) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logger = l
}

func (m *Manager) IsRunning(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.running[id]
	return ok
}

func (m *Manager) GetStats(id string) Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.running[id]
	if !ok || r.stats == nil {
		return Stats{}
	}
	return Stats{
		ActiveConns: atomic.LoadInt64(&r.stats.ActiveConns),
		TotalConns:  atomic.LoadInt64(&r.stats.TotalConns),
		BytesIn:     atomic.LoadInt64(&r.stats.BytesIn),
		BytesOut:    atomic.LoadInt64(&r.stats.BytesOut),
	}
}

func (m *Manager) Start(rule Rule) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.running[rule.ID]; ok {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	stats := &Stats{}
	tracker := newConnTracker()
	m.running[rule.ID] = &runningRule{
		cancel: cancel,
		stats:  stats,
		conns:  tracker,
	}

	listenHost := rule.ListenAddr
	if listenHost == "" {
		listenHost = "0.0.0.0"
	}
	listenAddr := net.JoinHostPort(listenHost, strconv.Itoa(rule.ListenPort))
	targetAddr := net.JoinHostPort(rule.TargetAddr, strconv.Itoa(rule.TargetPort))

	proto := strings.ToLower(rule.Protocol)
	if proto == "" {
		proto = "tcp"
	}

	if proto == "tcp" || proto == "tcp+udp" {
		ln, err := net.Listen("tcp", listenAddr)
		if err != nil {
			cancel()
			delete(m.running, rule.ID)
			return fmt.Errorf("listen tcp: %w", err)
		}
		go m.serveTCP(ctx, ln, targetAddr, stats, tracker, rule)
	}

	if proto == "udp" || proto == "tcp+udp" {
		uAddr, err := net.ResolveUDPAddr("udp", listenAddr)
		if err != nil {
			cancel()
			delete(m.running, rule.ID)
			return fmt.Errorf("resolve udp: %w", err)
		}
		uConn, err := net.ListenUDP("udp", uAddr)
		if err != nil {
			cancel()
			delete(m.running, rule.ID)
			return fmt.Errorf("listen udp: %w", err)
		}
		go m.serveUDP(ctx, uConn, targetAddr, stats, rule)
	}

	return nil
}

func (m *Manager) Stop(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.running[id]; ok {
		r.cancel()
		r.conns.closeAll()
		delete(m.running, id)
	}
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, r := range m.running {
		r.cancel()
		r.conns.closeAll()
		delete(m.running, id)
	}
}

func (m *Manager) serveTCP(ctx context.Context, ln net.Listener, target string, stats *Stats, tracker *connTracker, rule Rule) {
	defer ln.Close()
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}

		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.SetNoDelay(true)
			_ = tc.SetKeepAlive(true)
			_ = tc.SetKeepAlivePeriod(30 * time.Second)
		}

		tracker.add(conn)
		go func(c net.Conn) {
			start := time.Now()
			clientAddr := c.RemoteAddr().String()
			defer func() {
				tracker.remove(c)
				_ = c.Close()
			}()

			dialer := net.Dialer{
				Timeout:   3 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			remote, err := dialer.DialContext(ctx, "tcp", target)
			if err != nil {
				if m.logger != nil {
					m.logger.Record(LogEntry{
						ID:         strconv.FormatInt(time.Now().UnixNano(), 36),
						Time:       start,
						RuleID:     rule.ID,
						RuleName:   rule.Name,
						Protocol:   "tcp",
						ClientAddr: clientAddr,
						TargetAddr: target,
						DurationMS: time.Since(start).Milliseconds(),
						Status:     "error",
					})
				}
				return
			}
			if rc, ok := remote.(*net.TCPConn); ok {
				_ = rc.SetNoDelay(true)
			}
			tracker.add(remote)
			defer func() {
				tracker.remove(remote)
				_ = remote.Close()
			}()

			atomic.AddInt64(&stats.ActiveConns, 1)
			atomic.AddInt64(&stats.TotalConns, 1)
			defer atomic.AddInt64(&stats.ActiveConns, -1)

			var bytesIn, bytesOut int64
			var wg sync.WaitGroup
			wg.Add(2)

			go func() {
				defer wg.Done()
				n, _ := io.Copy(remote, c)
				atomic.AddInt64(&stats.BytesIn, n)
				atomic.AddInt64(&bytesIn, n)
				if tc, ok := remote.(*net.TCPConn); ok {
					_ = tc.CloseWrite()
				}
			}()

			go func() {
				defer wg.Done()
				n, _ := io.Copy(c, remote)
				atomic.AddInt64(&stats.BytesOut, n)
				atomic.AddInt64(&bytesOut, n)
				if tc, ok := c.(*net.TCPConn); ok {
					_ = tc.CloseWrite()
				}
			}()

			wg.Wait()

			if m.logger != nil {
				m.logger.Record(LogEntry{
					ID:         strconv.FormatInt(time.Now().UnixNano(), 36),
					Time:       start,
					RuleID:     rule.ID,
					RuleName:   rule.Name,
					Protocol:   "tcp",
					ClientAddr: clientAddr,
					TargetAddr: target,
					BytesIn:    bytesIn,
					BytesOut:   bytesOut,
					DurationMS: time.Since(start).Milliseconds(),
					Status:     "closed",
				})
			}
		}(conn)
	}
}

func (m *Manager) serveUDP(ctx context.Context, conn *net.UDPConn, target string, stats *Stats, rule Rule) {
	defer conn.Close()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	tAddr, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		return
	}

	buf := make([]byte, 65535)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}

		atomic.AddInt64(&stats.BytesIn, int64(n))
		atomic.AddInt64(&stats.TotalConns, 1)

		go func(data []byte, client *net.UDPAddr) {
			fwd, err := net.DialUDP("udp", nil, tAddr)
			if err != nil {
				return
			}
			defer fwd.Close()

			_, _ = fwd.Write(data)
			_ = fwd.SetReadDeadline(time.Now().Add(3 * time.Second))

			resp := make([]byte, 65535)
			rn, _, rerr := fwd.ReadFrom(resp)
			if rerr == nil && rn > 0 {
				atomic.AddInt64(&stats.BytesOut, int64(rn))
				_, _ = conn.WriteToUDP(resp[:rn], client)
			}
		}(append([]byte(nil), buf[:n]...), src)
	}
}

// SSE Hub
type SSEHub struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

func NewSSEHub() *SSEHub {
	return &SSEHub{clients: make(map[chan []byte]struct{})}
}

func (h *SSEHub) Subscribe() chan []byte {
	ch := make(chan []byte, 32)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *SSEHub) Unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	close(ch)
	h.mu.Unlock()
}

func (h *SSEHub) Broadcast(event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, b))

	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (h *SSEHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		close(ch)
	}
	h.clients = make(map[chan []byte]struct{})
}

// App REST server
type App struct {
	store   *ConfigStore
	manager *Manager
	logger  *TrafficLogger
	hub     *SSEHub
}

func NewApp(store *ConfigStore, manager *Manager, logger *TrafficLogger, hub *SSEHub) *App {
	return &App{store: store, manager: manager, logger: logger, hub: hub}
}

type ruleView struct {
	Rule
	Running bool  `json:"running"`
	Stats   Stats `json:"stats"`
}

func (a *App) ruleViews() []ruleView {
	cfg := a.store.Snapshot()
	views := make([]ruleView, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		views = append(views, ruleView{
			Rule:    r,
			Running: a.manager.IsRunning(r.ID),
			Stats:   a.manager.GetStats(r.ID),
		})
	}
	return views
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()

	subStatic, _ := fs.Sub(staticFS, "web/static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(subStatic))))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = tmpl.ExecuteTemplate(w, "index.html", map[string]any{
			"Rules": a.ruleViews(),
		})
	})

	mux.HandleFunc("/api/rules", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"rules": a.ruleViews(),
			})
		case http.MethodPost:
			var req Rule
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				_ = r.ParseForm()
				req.Name = r.FormValue("name")
				req.Protocol = r.FormValue("protocol")
				req.ListenAddr = r.FormValue("listen_addr")
				req.ListenPort, _ = strconv.Atoi(r.FormValue("listen_port"))
				req.TargetAddr = r.FormValue("target_addr")
				req.TargetPort, _ = strconv.Atoi(r.FormValue("target_port"))
			}
			if req.ListenPort <= 0 || req.ListenPort > 65535 || req.TargetPort <= 0 || req.TargetPort > 65535 {
				http.Error(w, `{"error":"invalid ports"}`, http.StatusBadRequest)
				return
			}
			if req.ListenAddr == "" {
				req.ListenAddr = "0.0.0.0"
			}
			if req.Protocol == "" {
				req.Protocol = "tcp"
			}
			if req.Name == "" {
				req.Name = fmt.Sprintf("%s:%d -> %s:%d", req.ListenAddr, req.ListenPort, req.TargetAddr, req.TargetPort)
			}
			req.Enabled = true
			if req.ID == "" {
				b := make([]byte, 8)
				_, _ = rand.Read(b)
				req.ID = hex.EncodeToString(b)
			}
			_ = a.store.AddRule(req)
			_ = a.manager.Start(req)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"rule":    req,
			})
		}
	})

	mux.HandleFunc("/api/rules/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/rules/"), "/")
		id := parts[0]

		if len(parts) > 1 && parts[1] == "toggle" && r.Method == http.MethodPost {
			updated, err := a.store.UpdateRule(id, func(rule *Rule) {
				rule.Enabled = !rule.Enabled
			})
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			if updated.Enabled {
				_ = a.manager.Start(updated)
			} else {
				a.manager.Stop(id)
			}
			_ = json.NewEncoder(w).Encode(updated)
			return
		}

		if r.Method == http.MethodDelete {
			a.manager.Stop(id)
			_ = a.store.DeleteRule(id)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	})

	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if a.logger == nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"logs": []LogEntry{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"logs": a.logger.RecentEntries(100)})
	})

	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		ch := a.hub.Subscribe()
		defer a.hub.Unsubscribe(ch)

		for {
			select {
			case <-r.Context().Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				_, _ = w.Write(msg)
				flusher.Flush()
			}
		}
	})

	return a.basicAuth(mux)
}

func (a *App) basicAuth(next http.Handler) http.Handler {
	user := os.Getenv("PATCHBAY_AUTH_USER")
	pass := os.Getenv("PATCHBAY_AUTH_PASS")
	if user == "" || pass == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(u), []byte(user)) != 1 ||
			subtle.ConstantTimeCompare([]byte(p), []byte(pass)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Patchbay"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Runtime App & Mobile Entry points
var (
	mobileMu     sync.Mutex
	mobileServer *http.Server
	mobileCancel context.CancelFunc
	mobileMgr    *Manager
	mobileStore  *ConfigStore
	mobileLogger *TrafficLogger
	mobileHub    *SSEHub
)

func Start(dataDir string, adminHost string, adminPort int) error {
	mobileMu.Lock()
	defer mobileMu.Unlock()

	if mobileServer != nil {
		return fmt.Errorf("patchbay is already running")
	}

	if dataDir == "" {
		dataDir = "."
	}
	_ = os.MkdirAll(dataDir, 0755)

	configPath := filepath.Join(dataDir, ConfigFileName)
	logDir := filepath.Join(dataDir, "logs")

	store, err := NewConfigStore(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	cfg := store.Snapshot()
	if adminPort > 0 && cfg.AdminPort != adminPort {
		_ = store.SetAdminPort(adminPort)
		cfg.AdminPort = adminPort
	}

	logger, _ := NewTrafficLogger(logDir, 1000)
	manager := NewManager()
	hub := NewSSEHub()
	if logger != nil {
		manager.SetLogger(logger)
		logger.SetOnRecord(func(entry LogEntry) {
			hub.Broadcast("log", entry)
		})
		logger.SetEnabled(cfg.IsLoggingEnabled())
	}

	if adminHost == "" {
		adminHost = "127.0.0.1"
	}
	addr := net.JoinHostPort(adminHost, strconv.Itoa(cfg.AdminPort))
	app := NewApp(store, manager, logger, hub)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen dashboard on %s: %w", addr, err)
	}

	srv := &http.Server{Addr: addr, Handler: app.Routes()}
	ctx, cancel := context.WithCancel(context.Background())

	for _, r := range cfg.Rules {
		if r.Enabled {
			_ = manager.Start(r)
		}
	}

	go func() {
		_ = srv.Serve(ln)
	}()

	// Periodic SSE broadcast
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hub.Broadcast("stats", app.ruleViews())
			}
		}
	}()

	mobileServer = srv
	mobileCancel = cancel
	mobileMgr = manager
	mobileStore = store
	mobileLogger = logger
	mobileHub = hub
	return nil
}

func Stop() {
	mobileMu.Lock()
	defer mobileMu.Unlock()

	if mobileCancel != nil {
		mobileCancel()
		mobileCancel = nil
	}
	if mobileServer != nil {
		_ = mobileServer.Close()
		mobileServer = nil
	}
	if mobileMgr != nil {
		mobileMgr.StopAll()
		mobileMgr = nil
	}
	if mobileHub != nil {
		mobileHub.Close()
		mobileHub = nil
	}
	if mobileLogger != nil {
		_ = mobileLogger.Close()
		mobileLogger = nil
	}
}

func IsRunning() bool {
	mobileMu.Lock()
	defer mobileMu.Unlock()
	return mobileServer != nil
}

func GetDashboardURL() string {
	mobileMu.Lock()
	defer mobileMu.Unlock()
	if mobileServer == nil {
		return ""
	}
	return "http://" + mobileServer.Addr + "/"
}
