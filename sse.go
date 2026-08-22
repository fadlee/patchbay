package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// SSEHub coordinates Server-Sent Events subscribers and broadcasts typed events.
type SSEHub struct {
	mu          sync.Mutex
	clients     map[chan []byte]struct{}
	broadcastCh chan []byte
	quitCh      chan struct{}
}

// NewSSEHub creates and starts an active SSE event broadcaster.
func NewSSEHub() *SSEHub {
	h := &SSEHub{
		clients:     make(map[chan []byte]struct{}),
		broadcastCh: make(chan []byte, 100),
		quitCh:      make(chan struct{}),
	}
	go h.run()
	return h
}

func (h *SSEHub) run() {
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-h.quitCh:
			h.mu.Lock()
			for ch := range h.clients {
				close(ch)
			}
			h.clients = make(map[chan []byte]struct{})
			h.mu.Unlock()
			return

		case msg := <-h.broadcastCh:
			h.mu.Lock()
			for ch := range h.clients {
				select {
				case ch <- msg:
				default:
					// Drop or slow consumer
				}
			}
			h.mu.Unlock()

		case <-heartbeat.C:
			h.mu.Lock()
			for ch := range h.clients {
				select {
				case ch <- []byte(": heartbeat\n\n"):
				default:
				}
			}
			h.mu.Unlock()
		}
	}
}

// Broadcast serializes data to JSON and publishes a named SSE event to all connected clients.
func (h *SSEHub) Broadcast(eventName string, data any) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", eventName, string(bytes)))
	select {
	case h.broadcastCh <- msg:
	case <-h.quitCh:
	default:
	}
}

// Handler returns an http.HandlerFunc streaming events to client with text/event-stream.
func (h *SSEHub) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		clientCh := make(chan []byte, 32)
		h.mu.Lock()
		h.clients[clientCh] = struct{}{}
		h.mu.Unlock()

		defer func() {
			h.mu.Lock()
			delete(h.clients, clientCh)
			h.mu.Unlock()
		}()

		// Send initial connection comment
		_, _ = fmt.Fprint(w, ": connected\n\n")
		flusher.Flush()

		notify := r.Context().Done()
		for {
			select {
			case <-notify:
				return
			case msg, ok := <-clientCh:
				if !ok {
					return
				}
				_, _ = w.Write(msg)
				flusher.Flush()
			}
		}
	}
}

// Close terminates the SSEHub broadcaster.
func (h *SSEHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-h.quitCh:
	default:
		close(h.quitCh)
	}
}
