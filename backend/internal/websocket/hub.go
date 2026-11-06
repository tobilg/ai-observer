package websocket

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/tobilg/ai-observer/internal/logger"
)

// Hub maintains the set of active clients and broadcasts messages to them.
type Hub struct {
	// Registered clients
	clients map[*Client]bool

	// Inbound messages to broadcast
	broadcast chan Message

	// Register requests from clients
	register chan *Client

	// Unregister requests from clients
	unregister chan *Client

	// Mutex for client map
	mu sync.RWMutex

	// originAllowed decides whether a handshake's Origin is accepted
	originAllowed func(r *http.Request) bool

	// done is closed by Stop
	done     chan struct{}
	stopOnce sync.Once
}

// NewHub creates a new Hub instance. Without SetOriginCheck, only same-origin
// handshakes are accepted, like gorilla's default.
func NewHub() *Hub {
	return &Hub{
		clients:       make(map[*Client]bool),
		broadcast:     make(chan Message, 256),
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		originAllowed: sameOrigin,
		done:          make(chan struct{}),
	}
}

// SetOriginCheck configures which origins may open WebSocket connections.
// Call it before serving.
func (h *Hub) SetOriginCheck(allowed func(r *http.Request) bool) {
	h.originAllowed = allowed
}

// Stop ends Run and closes all client connections
func (h *Hub) Stop() {
	h.stopOnce.Do(func() { close(h.done) })
}

// Run starts the hub's main loop. It returns after Stop.
func (h *Hub) Run() {
	for {
		select {
		case <-h.done:
			h.mu.Lock()
			for client := range h.clients {
				delete(h.clients, client)
				client.Close() // writePump sends a close frame and ends the connection
			}
			h.mu.Unlock()
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			count := len(h.clients)
			h.mu.Unlock()
			logger.Debug("WebSocket client connected", "total_clients", count)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.Close() // Safe to call multiple times
			}
			count := len(h.clients)
			h.mu.Unlock()
			logger.Debug("WebSocket client disconnected", "total_clients", count)

		case message := <-h.broadcast:
			data, err := json.Marshal(message)
			if err != nil {
				logger.Error("Error marshaling WebSocket message", "error", err)
				continue
			}

			h.mu.RLock()
			// Collect clients that need to be disconnected
			var toDisconnect []*Client
			for client := range h.clients {
				select {
				case client.send <- data:
				default:
					// Client buffer full, mark for disconnect
					toDisconnect = append(toDisconnect, client)
				}
			}
			h.mu.RUnlock()

			// Disconnect clients with full buffers. Run is the only reader of
			// h.unregister, so they are removed here instead of sent there.
			if len(toDisconnect) > 0 {
				h.mu.Lock()
				for _, c := range toDisconnect {
					if _, ok := h.clients[c]; ok {
						delete(h.clients, c)
						c.Close() // writePump sends a close frame and ends the connection
					}
				}
				h.mu.Unlock()
				logger.Warn("Disconnected slow WebSocket clients", "count", len(toDisconnect))
			}
		}
	}
}

// Broadcast sends a message to all connected clients.
func (h *Hub) Broadcast(msg Message) {
	select {
	case <-h.done:
		return
	default:
	}
	select {
	case h.broadcast <- msg:
	default:
		logger.Warn("Broadcast channel full, dropping message", "message_type", msg.Type)
	}
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
