package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// throttleInterval is the minimum time between deliveries of throttled event types.
	throttleInterval = 250 * time.Millisecond

	// clientSendBuffer is the number of messages buffered per client before the client
	// is considered slow and dropped.
	clientSendBuffer = 256

	// writeWait is the deadline for a single WebSocket write.
	writeWait = 10 * time.Second
)

// throttledEventTypes is the set of event types that are subject to the 250 ms throttle.
var throttledEventTypes = map[string]bool{
	"price_update": true,
	"spread_stats": true,
}

// Event is a server-sent message envelope.
type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// client represents a single connected WebSocket client.
type client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// Hub maintains the set of active clients and broadcasts events.
type Hub struct {
	// Inbound events from the application.
	publish chan Event

	// Register and unregister requests from clients.
	register   chan *client
	unregister chan *client

	// clients holds all currently connected clients.
	clients map[*client]struct{}
}

// NewHub creates a Hub ready to use. Call Run() in a separate goroutine.
func NewHub() *Hub {
	return &Hub{
		publish:    make(chan Event, 512),
		register:   make(chan *client, 64),
		unregister: make(chan *client, 64),
		clients:    make(map[*client]struct{}),
	}
}

// Publish queues an event for broadcast. Non-blocking: drops the event if the hub
// publish channel is full (back-pressure protection).
func (h *Hub) Publish(ev Event) {
	select {
	case h.publish <- ev:
	default:
		slog.Warn("hub publish channel full, dropping event", "type", ev.Type)
	}
}

// Run is the hub event loop. It must be called exactly once in a dedicated goroutine.
// It manages client registration, throttled and immediate broadcast.
func (h *Hub) Run() {
	// Pending throttled events (latest value per type).
	pending := make(map[string]*Event)
	ticker := time.NewTicker(throttleInterval)
	defer ticker.Stop()

	for {
		select {
		case c := <-h.register:
			h.clients[c] = struct{}{}

		case c := <-h.unregister:
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
			}

		case ev := <-h.publish:
			if throttledEventTypes[ev.Type] {
				// Store only the latest value for this event type.
				cp := ev
				pending[ev.Type] = &cp
			} else {
				// Immediate delivery.
				h.broadcast(&ev)
			}

		case <-ticker.C:
			// Flush all pending throttled events.
			for _, ev := range pending {
				h.broadcast(ev)
			}
			// Reset pending map.
			pending = make(map[string]*Event)
		}
	}
}

// broadcast serialises ev and sends it to all connected clients.
// Slow clients (full send buffer) are dropped immediately.
func (h *Hub) broadcast(ev *Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		slog.Error("hub: failed to marshal event", "type", ev.Type, "err", err)
		return
	}

	for c := range h.clients {
		select {
		case c.send <- data:
		default:
			// Client send buffer is full — drop it.
			delete(h.clients, c)
			close(c.send)
		}
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow all origins for the challenge; production should check origin.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ServeWS upgrades an HTTP connection to WebSocket and registers the new client.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("ws upgrade failed", "err", err)
		return
	}

	c := &client{
		hub:  h,
		conn: conn,
		send: make(chan []byte, clientSendBuffer),
	}
	h.register <- c

	// writePump runs in a goroutine and serialises writes to the WebSocket connection.
	go c.writePump()

	// readPump drains inbound frames and detects disconnection.
	go c.readPump()
}

// readPump drains inbound WebSocket frames and signals disconnect via unregister.
func (c *client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			break
		}
	}
}

// writePump serialises all outbound writes and flushes the send channel on close.
func (c *client) writePump() {
	defer c.conn.Close()

	for msg := range c.send {
		c.conn.SetWriteDeadline(time.Now().Add(writeWait)) //nolint:errcheck
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}
