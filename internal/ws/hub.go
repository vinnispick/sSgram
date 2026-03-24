package ws

import (
	"log"
	"sync"

	"github.com/gofiber/contrib/websocket"
)

// Client represents a connected WebSocket client.
type Client struct {
	Conn     *websocket.Conn
	Username string
}

// Hub maintains the set of active clients and broadcasts messages.
type Hub struct {
	clients    map[*Client]bool
	Broadcast  chan []byte
	Register   chan *Client
	Unregister chan *Client
	mu         sync.RWMutex
}

// NewHub creates a new Hub instance.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		Broadcast:  make(chan []byte, 256),
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
			log.Printf("👤 Client connected: %s (total: %d)", client.Username, len(h.clients))

		case client := <-h.Unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.Conn.Close()
			}
			h.mu.Unlock()
			log.Printf("👤 Client disconnected: %s (total: %d)", client.Username, len(h.clients))

		case message := <-h.Broadcast:
			h.mu.RLock()
			for client := range h.clients {
				if err := client.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
					log.Printf("⚠️  Write error for %s: %v", client.Username, err)
					client.Conn.Close()
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// OnlineUsers returns the list of currently connected usernames.
func (h *Hub) OnlineUsers() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	seen := make(map[string]bool)
	users := []string{}
	for client := range h.clients {
		if !seen[client.Username] {
			seen[client.Username] = true
			users = append(users, client.Username)
		}
	}
	return users
}
