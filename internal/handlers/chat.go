package handlers

import (
	"fmt"
	"html"
	"log"
	"time"

	"ssgram/internal/database"
	"ssgram/internal/models"
	"ssgram/internal/ws"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

// ChatHub is the global WebSocket hub reference (set from main).
var ChatHub *ws.Hub

// ShowChat renders the chat page with message history.
func ShowChat(c *fiber.Ctx) error {
	username := c.Cookies("username")
	if username == "" {
		return c.Redirect("/")
	}

	// Load last 50 messages
	var messages []models.Message
	database.DB.Order("created_at ASC").Limit(50).Find(&messages)

	return c.Render("chat", fiber.Map{
		"Username": username,
		"Messages": messages,
	})
}

// SendMessage saves a message to the DB and broadcasts it via WebSocket.
func SendMessage(c *fiber.Ctx) error {
	username := c.Cookies("username")
	if username == "" {
		return c.Status(fiber.StatusUnauthorized).SendString("Not logged in")
	}

	content := c.FormValue("content")
	if content == "" {
		return c.SendStatus(fiber.StatusNoContent)
	}

	// Save to database
	msg := models.Message{
		Username:  username,
		Content:   content,
		CreatedAt: time.Now(),
	}
	database.DB.Create(&msg)

	// Build HTML fragment and broadcast
	htmlFragment := renderMessageHTML(msg, "")
	ChatHub.Broadcast <- []byte(htmlFragment)

	return c.SendStatus(fiber.StatusNoContent)
}

// HandleWebSocket upgrades the connection and registers the client with the hub.
func HandleWebSocket(c *websocket.Conn) {
	username := c.Cookies("username")
	if username == "" {
		c.Close()
		return
	}

	client := &ws.Client{
		Conn:     c,
		Username: username,
	}

	ChatHub.Register <- client

	defer func() {
		ChatHub.Unregister <- client
	}()

	// Read loop — keeps the connection alive, handles client-sent messages
	for {
		_, _, err := c.ReadMessage()
		if err != nil {
			log.Printf("🔌 WebSocket read error for %s: %v", username, err)
			break
		}
	}
}

// renderMessageHTML builds an HTML fragment for a single chat message.
func renderMessageHTML(msg models.Message, currentUser string) string {
	escapedUser := html.EscapeString(msg.Username)
	escapedContent := html.EscapeString(msg.Content)
	timeStr := msg.CreatedAt.Format("15:04")

	return fmt.Sprintf(
		`<div id="message-%d" class="message-bubble animate-in" hx-swap-oob="beforeend:#messages">
			<div class="message-header">
				<span class="message-username">%s</span>
				<span class="message-time">%s</span>
			</div>
			<div class="message-content">%s</div>
		</div>`,
		msg.ID, escapedUser, timeStr, escapedContent,
	)
}
