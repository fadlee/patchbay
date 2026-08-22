package main

import (
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

func TestManagerStopClosesActiveTCPConnections(t *testing.T) {
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen target: %v", err)
	}
	defer target.Close()

	targetConn := make(chan net.Conn, 1)
	go func() {
		conn, err := target.Accept()
		if err == nil {
			targetConn <- conn
		}
	}()

	listenPort := freeTCPPort(t)
	manager := NewManager()
	rule := Rule{
		ID:         "active-tcp-test",
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

	client, err := net.Dial("tcp", net.JoinHostPort(rule.ListenAddr, itoa(listenPort)))
	if err != nil {
		t.Fatalf("dial forwarded port: %v", err)
	}
	defer client.Close()

	var upstream net.Conn
	select {
	case upstream = <-targetConn:
	case <-time.After(time.Second):
		t.Fatal("target did not receive forwarded connection")
	}
	defer upstream.Close()

	manager.Stop(rule.ID)
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	_, err = client.Read(make([]byte, 1))
	if err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("active client connection remained open after Stop: %v", err)
	}
	if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		t.Logf("connection closed with expected network error: %v", err)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve TCP port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release TCP port: %v", err)
	}
	return port
}
