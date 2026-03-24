package ws

import (
	"log"
	"sync"

	"github.com/gofiber/contrib/websocket"
)

// Client represents a connected WebSocket client.
type Client struct {
	Conn     *websocket.Conn
	UserID   uint
	Username string
}

// Hub maintains the set of active clients and delivers messages.
type Hub struct {
	clients    map[*Client]bool
	Register   chan *Client
	Unregister chan *Client
	mu         sync.RWMutex
}

// NewHub creates a new Hub instance.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
	}
}

// Run starts the hub's event loop. Should be called as a goroutine.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("👤 Client connected: %s (id:%d, total: %d)", client.Username, client.UserID, len(h.clients))

		case client := <-h.Unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.Conn.Close()
			}
			h.mu.Unlock()
			log.Printf("👤 Client disconnected: %s (total: %d)", client.Username, len(h.clients))
		}
	}
}

// SendToUsers delivers a message only to clients with matching user IDs.
func (h *Hub) SendToUsers(userIDs []uint, data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	idSet := make(map[uint]bool, len(userIDs))
	for _, id := range userIDs {
		idSet[id] = true
	}

	for client := range h.clients {
		if idSet[client.UserID] {
			if err := client.Conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("⚠️  Write error for %s: %v", client.Username, err)
				client.Conn.Close()
				delete(h.clients, client)
			}
		}
	}
}

// OnlineUserIDs returns the set of currently connected user IDs.
func (h *Hub) OnlineUserIDs() map[uint]bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	ids := make(map[uint]bool)
	for client := range h.clients {
		ids[client.UserID] = true
	}
	return ids
}
