package main

import (
	"context"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Stats holds live counters for a running rule.
type Stats struct {
	ActiveConns int64
	TotalConns  int64
	BytesIn     int64
	BytesOut    int64
}

// runningRule tracks the goroutines/listeners backing one active Rule.
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
	for _, conn := range conns {
		conn.Close()
	}
}

// Manager owns all currently-running forwarders, keyed by rule ID.
type Manager struct {
	mu         sync.Mutex
	running    map[string]*runningRule
	fwWarnings map[string]string
	logger     *TrafficLogger
}

func NewManager() *Manager {
	return &Manager{
		running:    make(map[string]*runningRule),
		fwWarnings: make(map[string]string),
	}
}

func NewManagerWithLogger(logger *TrafficLogger) *Manager {
	return &Manager{
		running:    make(map[string]*runningRule),
		fwWarnings: make(map[string]string),
		logger:     logger,
	}
}

func (m *Manager) SetLogger(logger *TrafficLogger) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logger = logger
}

// FirewallWarning returns a human-readable warning if the firewall rule for
// this rule ID failed to apply (e.g. not running as Administrator), or an
// empty string if there's no problem to report.
func (m *Manager) FirewallWarning(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fwWarnings[id]
}

// IsRunning reports whether a rule currently has active listeners.
func (m *Manager) IsRunning(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.running[id]
	return ok
}

// StatsFor returns a snapshot of stats for a rule, or zero value if not running.
func (m *Manager) StatsFor(id string) Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	rr, ok := m.running[id]
	if !ok {
		return Stats{}
	}
	return Stats{
		ActiveConns: atomic.LoadInt64(&rr.stats.ActiveConns),
		TotalConns:  atomic.LoadInt64(&rr.stats.TotalConns),
		BytesIn:     atomic.LoadInt64(&rr.stats.BytesIn),
		BytesOut:    atomic.LoadInt64(&rr.stats.BytesOut),
	}
}

// Start begins forwarding for a rule. No-op if already running.
func (m *Manager) Start(r Rule) error {
	m.mu.Lock()
	if _, ok := m.running[r.ID]; ok {
		m.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	stats := &Stats{}
	rr := &runningRule{cancel: cancel, stats: stats, conns: newConnTracker()}
	m.running[r.ID] = rr
	m.mu.Unlock()

	listenAddr := r.ListenAddr
	if listenAddr == "" {
		listenAddr = "0.0.0.0"
	}

	if r.Protocol == "tcp" || r.Protocol == "tcp+udp" {
		if err := startTCP(ctx, r, listenAddr, r.ListenPort, r.TargetAddr, r.TargetPort, stats, rr.conns, m.logger); err != nil {
			m.Stop(r.ID)
			return err
		}
	}
	if r.Protocol == "udp" || r.Protocol == "tcp+udp" {
		if err := startUDP(ctx, r, listenAddr, r.ListenPort, r.TargetAddr, r.TargetPort, stats, rr.conns, m.logger); err != nil {
			m.Stop(r.ID)
			return err
		}
	}

	fwErr := ensureFirewallRule(r)
	m.mu.Lock()
	if fwErr != nil {
		m.fwWarnings[r.ID] = "firewall rule not applied — run as Administrator (or install as a service)"
	} else {
		delete(m.fwWarnings, r.ID)
	}
	m.mu.Unlock()

	return nil
}

// Stop halts forwarding for a rule and closes all active proxy connections.
func (m *Manager) Stop(id string) {
	m.mu.Lock()
	rr, ok := m.running[id]
	if ok {
		delete(m.running, id)
	}
	delete(m.fwWarnings, id)
	m.mu.Unlock()
	if ok {
		rr.cancel()
		rr.conns.closeAll()
	}
	removeFirewallRule(id)
}

// StopAll halts every running forwarder (used on shutdown).
func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.running))
	for id := range m.running {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

func startTCP(ctx context.Context, r Rule, listenAddr string, listenPort int, targetAddr string, targetPort int, stats *Stats, tracker *connTracker, logger *TrafficLogger) error {
	addr := net.JoinHostPort(listenAddr, itoa(listenPort))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	target := net.JoinHostPort(targetAddr, itoa(targetPort))

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					log.Printf("tcp accept error on %s: %v", addr, err)
					return
				}
			}
			tracker.add(conn)
			go handleTCPConn(ctx, r, conn, target, stats, tracker, logger)
		}
	}()
	return nil
}

func handleTCPConn(ctx context.Context, r Rule, src net.Conn, target string, stats *Stats, tracker *connTracker, logger *TrafficLogger) {
	startTime := time.Now()
	clientAddr := src.RemoteAddr().String()
	sessionID := newID()

	defer func() {
		tracker.remove(src)
		src.Close()
	}()

	dialer := net.Dialer{Timeout: 5 * time.Second}
	dst, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		log.Printf("tcp dial %s failed: %v", target, err)
		if logger != nil {
			logger.Record(LogEntry{
				ID:         sessionID,
				Time:       startTime,
				RuleID:     r.ID,
				RuleName:   r.Name,
				Protocol:   "tcp",
				ClientAddr: clientAddr,
				TargetAddr: target,
				DurationMS: time.Since(startTime).Milliseconds(),
				Status:     "error",
			})
		}
		return
	}
	tracker.add(dst)
	defer func() {
		tracker.remove(dst)
		dst.Close()
	}()

	atomic.AddInt64(&stats.ActiveConns, 1)
	atomic.AddInt64(&stats.TotalConns, 1)
	defer atomic.AddInt64(&stats.ActiveConns, -1)

	var bytesIn, bytesOut int64
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		n, _ := io.Copy(dst, src)
		atomic.AddInt64(&stats.BytesIn, n)
		atomic.AddInt64(&bytesIn, n)
		if tc, ok := dst.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		n, _ := io.Copy(src, dst)
		atomic.AddInt64(&stats.BytesOut, n)
		atomic.AddInt64(&bytesOut, n)
		if tc, ok := src.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()
	wg.Wait()

	if logger != nil {
		logger.Record(LogEntry{
			ID:         sessionID,
			Time:       startTime,
			RuleID:     r.ID,
			RuleName:   r.Name,
			Protocol:   "tcp",
			ClientAddr: clientAddr,
			TargetAddr: target,
			BytesIn:    bytesIn,
			BytesOut:   bytesOut,
			DurationMS: time.Since(startTime).Milliseconds(),
			Status:     "closed",
		})
	}
}

// udpSession tracks one client<->target relay so replies route back correctly.
type udpSession struct {
	targetConn *net.UDPConn
	lastActive time.Time
}

func startUDP(ctx context.Context, r Rule, listenAddr string, listenPort int, targetAddr string, targetPort int, stats *Stats, tracker *connTracker, logger *TrafficLogger) error {
	laddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(listenAddr, itoa(listenPort)))
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return err
	}
	tracker.add(conn)

	targetStr := net.JoinHostPort(targetAddr, itoa(targetPort))
	taddr, err := net.ResolveUDPAddr("udp", targetStr)
	if err != nil {
		conn.Close()
		tracker.remove(conn)
		return err
	}

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	sessions := make(map[string]*udpSession)
	var sessMu sync.Mutex

	// Periodically clean up idle sessions (5 min timeout).
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sessMu.Lock()
				for k, s := range sessions {
					if time.Since(s.lastActive) > 5*time.Minute {
						s.targetConn.Close()
						tracker.remove(s.targetConn)
						delete(sessions, k)
					}
				}
				sessMu.Unlock()
			}
		}
	}()

	go func() {
		buf := make([]byte, 65535)
		for {
			n, clientAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					log.Printf("udp read error: %v", err)
					return
				}
			}
			atomic.AddInt64(&stats.BytesIn, int64(n))

			key := clientAddr.String()
			sessMu.Lock()
			sess, ok := sessions[key]
			if !ok {
				tc, err := net.DialUDP("udp", nil, taddr)
				if err != nil {
					sessMu.Unlock()
					log.Printf("udp dial target failed: %v", err)
					continue
				}
				tracker.add(tc)
				sess = &udpSession{targetConn: tc}
				sessions[key] = sess
				atomic.AddInt64(&stats.TotalConns, 1)
				atomic.AddInt64(&stats.ActiveConns, 1)

				// Relay replies from target back to this client.
				go func(client *net.UDPAddr, tc *net.UDPConn, key string) {
					defer tracker.remove(tc)
					rbuf := make([]byte, 65535)
					for {
						n, err := tc.Read(rbuf)
						if err != nil {
							return
						}
						atomic.AddInt64(&stats.BytesOut, int64(n))
						conn.WriteToUDP(rbuf[:n], client)
					}
				}(clientAddr, tc, key)
			}
			sess.lastActive = time.Now()
			sessMu.Unlock()

			data := append([]byte(nil), buf[:n]...)
			sess.targetConn.Write(data)
		}
	}()

	return nil
}

func itoa(i int) string {
	return strconv.Itoa(i)
}
