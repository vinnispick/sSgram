package handlers

import (
	"fmt"
	"html"
	"log"
	"strconv"
	"time"

	"ssgram/internal/database"
	"ssgram/internal/models"
	"ssgram/internal/ws"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

// ChatHub is the global WebSocket hub reference (set from main).
var ChatHub *ws.Hub

// getCurrentUser extracts the current user from cookies.
func getCurrentUser(c *fiber.Ctx) (uint, string) {
	idStr := c.Cookies("user_id")
	username := c.Cookies("username")
	if idStr == "" || username == "" {
		return 0, ""
	}
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return 0, ""
	}
	return uint(id), username
}

// ShowChat renders the main layout with contact sidebar.
func ShowChat(c *fiber.Ctx) error {
	userID, username := getCurrentUser(c)
	if userID == 0 {
		return c.Redirect("/")
	}

	// Get all users except current
	var contacts []models.User
	database.DB.Where("id != ?", userID).Order("username ASC").Find(&contacts)

	// Get online user IDs
	onlineIDs := ChatHub.OnlineUserIDs()

	type ContactView struct {
		ID       uint
		Username string
		Online   bool
	}
	contactViews := make([]ContactView, len(contacts))
	for i, u := range contacts {
		contactViews[i] = ContactView{
			ID:       u.ID,
			Username: u.Username,
			Online:   onlineIDs[u.ID],
		}
	}

	return c.Render("chat", fiber.Map{
		"UserID":   userID,
		"Username": username,
		"Contacts": contactViews,
	})
}

// ShowConversation returns the HTMX partial for a 1-on-1 conversation.
func ShowConversation(c *fiber.Ctx) error {
	userID, _ := getCurrentUser(c)
	if userID == 0 {
		return c.Status(fiber.StatusUnauthorized).SendString("Not logged in")
	}

	partnerID, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid user ID")
	}

	// Get partner info
	var partner models.User
	if err := database.DB.First(&partner, partnerID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).SendString("User not found")
	}

	// Fetch messages between the two users
	var messages []models.Message
	database.DB.Preload("Sender").
		Where("(sender_id = ? AND receiver_id = ?) OR (sender_id = ? AND receiver_id = ?)",
			userID, partnerID, partnerID, userID).
		Order("created_at ASC").
		Limit(100).
		Find(&messages)

	return c.Render("conversation", fiber.Map{
		"UserID":     userID,
		"PartnerID":  partner.ID,
		"PartnerName": partner.Username,
		"Messages":   messages,
	})
}

// SendMessage saves a message and delivers it to sender + receiver.
func SendMessage(c *fiber.Ctx) error {
	userID, _ := getCurrentUser(c)
	if userID == 0 {
		return c.Status(fiber.StatusUnauthorized).SendString("Not logged in")
	}

	receiverIDStr := c.FormValue("receiver_id")
	content := c.FormValue("content")
	if receiverIDStr == "" || content == "" {
		return c.SendStatus(fiber.StatusNoContent)
	}

	receiverID, err := strconv.ParseUint(receiverIDStr, 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid receiver")
	}

	// Save to database
	msg := models.Message{
		SenderID:   userID,
		ReceiverID: uint(receiverID),
		Content:    content,
		CreatedAt:  time.Now(),
	}
	database.DB.Create(&msg)

	// Load sender relation for rendering
	database.DB.Preload("Sender").First(&msg, msg.ID)

	// Send to receiver via WebSocket (with OOB for auto-append)
	receiverHTML := renderMessageHTMLOOB(msg, uint(receiverID))
	ChatHub.SendToUsers([]uint{uint(receiverID)}, []byte(receiverHTML))

	// Return sender's own bubble via HTMX response
	senderHTML := renderMessageHTML(msg, userID)
	c.Set("Content-Type", "text/html")
	return c.SendString(senderHTML)
}

// HandleWebSocket upgrades the connection and registers the client.
func HandleWebSocket(c *websocket.Conn) {
	idStr := c.Cookies("user_id")
	username := c.Cookies("username")
	if idStr == "" {
		c.Close()
		return
	}
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.Close()
		return
	}

	client := &ws.Client{
		Conn:     c,
		UserID:   uint(id),
		Username: username,
	}

	ChatHub.Register <- client

	defer func() {
		ChatHub.Unregister <- client
	}()

	// Read loop — keeps connection alive
	for {
		_, _, err := c.ReadMessage()
		if err != nil {
			log.Printf("🔌 WebSocket read error for %s: %v", username, err)
			break
		}
	}
}

// renderMessageHTML builds an HTML fragment with alignment based on viewer.
// If oob is true, adds hx-swap-oob for WebSocket delivery.
func renderMessageHTML(msg models.Message, viewerID uint) string {
	return buildMessageHTML(msg, viewerID, false)
}

func renderMessageHTMLOOB(msg models.Message, viewerID uint) string {
	return buildMessageHTML(msg, viewerID, true)
}

func buildMessageHTML(msg models.Message, viewerID uint, oob bool) string {
	escapedUser := html.EscapeString(msg.Sender.Username)
	escapedContent := html.EscapeString(msg.Content)
	timeStr := msg.CreatedAt.Format("15:04")

	isMine := msg.SenderID == viewerID

	bubbleClass := "message-theirs"
	alignClass := "justify-start"
	if isMine {
		bubbleClass = "message-mine"
		alignClass = "justify-end"
	}

	oobAttr := ""
	if oob {
		oobAttr = ` hx-swap-oob="beforeend:#messages"`
	}

	return fmt.Sprintf(
		`<div id="message-%d" class="flex %s animate-in"%s>
			<div class="%s">
				<div class="message-header">
					<span class="message-username">%s</span>
					<span class="message-time">%s</span>
				</div>
				<div class="message-content">%s</div>
			</div>
		</div>`,
		msg.ID, alignClass, oobAttr, bubbleClass, escapedUser, timeStr, escapedContent,
	)
}
