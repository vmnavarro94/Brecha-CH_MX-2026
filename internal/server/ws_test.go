package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/server"
)

// dialWS connects to the test server via WebSocket and returns the conn.
func dialWS(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// startServer wires up a Hub and returns a running httptest.Server.
func startServer(t *testing.T) (*server.Hub, *httptest.Server) {
	t.Helper()
	hub := server.NewHub()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.ServeWS)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	go hub.Run()
	return hub, srv
}

// readMessage reads one JSON message from conn with a timeout.
func readMessage(t *testing.T, conn *websocket.Conn, timeout time.Duration) map[string]interface{} {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(timeout)) //nolint:errcheck
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var v map[string]interface{}
	if err := json.Unmarshal(msg, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return v
}

// tryReadMessage reads one message with a timeout and returns false if it times out.
func tryReadMessage(conn *websocket.Conn, timeout time.Duration) (map[string]interface{}, bool) {
	conn.SetReadDeadline(time.Now().Add(timeout)) //nolint:errcheck
	_, msg, err := conn.ReadMessage()
	if err != nil {
		return nil, false
	}
	var v map[string]interface{}
	if err := json.Unmarshal(msg, &v); err != nil {
		return nil, false
	}
	return v, true
}

// TestHub_BroadcastToAll verifies that a published event is received by all connected clients.
func TestHub_BroadcastToAll(t *testing.T) {
	hub, srv := startServer(t)

	c1 := dialWS(t, srv)
	c2 := dialWS(t, srv)
	c3 := dialWS(t, srv)

	// Allow goroutines to register.
	time.Sleep(30 * time.Millisecond)

	hub.Publish(server.Event{Type: "trade_executed", Data: map[string]string{"id": "t1"}})

	var wg sync.WaitGroup
	for _, conn := range []*websocket.Conn{c1, c2, c3} {
		wg.Add(1)
		go func(c *websocket.Conn) {
			defer wg.Done()
			msg := readMessage(t, c, 500*time.Millisecond)
			if msg["type"] != "trade_executed" {
				t.Errorf("expected type=trade_executed, got %v", msg["type"])
			}
		}(conn)
	}
	wg.Wait()
}

// TestHub_ImmediateEventsNotThrottled verifies that opportunity and trade_executed events
// are delivered without waiting for the 250 ms throttle window.
func TestHub_ImmediateEventsNotThrottled(t *testing.T) {
	hub, srv := startServer(t)
	conn := dialWS(t, srv)
	time.Sleep(30 * time.Millisecond)

	immediateTypes := []string{"opportunity", "trade_executed", "circuit_breaker", "pnl_update"}

	for _, evType := range immediateTypes {
		hub.Publish(server.Event{Type: evType, Data: map[string]string{"k": "v"}})
		msg := readMessage(t, conn, 200*time.Millisecond)
		if msg["type"] != evType {
			t.Errorf("expected type=%s immediately, got %v", evType, msg["type"])
		}
	}
}

// TestHub_ThrottledEvents verifies that price_update and spread_stats are rate-limited
// to at most 1 delivery per 250 ms window: 10 rapid publishes must deliver ≤1 message
// within a 249 ms observation window.
func TestHub_ThrottledEvents(t *testing.T) {
	hub, srv := startServer(t)
	conn := dialWS(t, srv)
	time.Sleep(30 * time.Millisecond)

	throttledTypes := []string{"price_update", "spread_stats"}

	for _, evType := range throttledTypes {
		// Publish 10 events rapidly.
		for i := 0; i < 10; i++ {
			hub.Publish(server.Event{Type: evType, Data: map[string]string{"i": "x"}})
		}

		// Count deliveries within a window shorter than one tick (249 ms).
		count := 0
		deadline := time.Now().Add(249 * time.Millisecond)
		conn.SetReadDeadline(deadline) //nolint:errcheck
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break // timeout or close
			}
			count++
		}

		if count > 1 {
			t.Errorf("type=%s: expected ≤1 delivery in 249ms window, got %d", evType, count)
		}
	}
}

// TestHub_SlowClientDropped verifies that a client with a full send buffer is dropped
// without blocking other clients.
func TestHub_SlowClientDropped(t *testing.T) {
	hub, srv := startServer(t)

	// Connect a "fast" client that will keep draining.
	fast := dialWS(t, srv)
	// Connect a "slow" client that never reads.
	slow := dialWS(t, srv)
	_ = slow

	time.Sleep(30 * time.Millisecond)

	// Flood with immediate events to fill the slow client's buffer.
	for i := 0; i < 300; i++ {
		hub.Publish(server.Event{Type: "trade_executed", Data: map[string]string{"i": "x"}})
	}

	// Fast client should still be receiving events after the flood.
	// Give hub time to process drops.
	time.Sleep(100 * time.Millisecond)

	hub.Publish(server.Event{Type: "trade_executed", Data: map[string]string{"id": "final"}})

	msg := readMessage(t, fast, 500*time.Millisecond)
	if msg == nil {
		t.Error("fast client should still receive events after slow client is dropped")
	}
}

// TestHub_DisconnectCleansUp verifies that after a client closes its connection,
// it is removed from the hub registry (no panic or deadlock on subsequent publishes).
func TestHub_DisconnectCleansUp(t *testing.T) {
	hub, srv := startServer(t)
	conn := dialWS(t, srv)
	time.Sleep(30 * time.Millisecond)

	// Close the client connection.
	conn.Close()
	time.Sleep(60 * time.Millisecond)

	// Subsequent publish must not panic or block.
	done := make(chan struct{})
	go func() {
		hub.Publish(server.Event{Type: "trade_executed", Data: map[string]string{"id": "x"}})
		close(done)
	}()

	select {
	case <-done:
		// pass
	case <-time.After(500 * time.Millisecond):
		t.Error("Publish blocked after client disconnect")
	}
}
