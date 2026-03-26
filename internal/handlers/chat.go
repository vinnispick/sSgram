package handlers

import (
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"ssgram/internal/database"
	"ssgram/internal/models"
	"ssgram/internal/ws"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
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

// ContactView is a UI-specific representation of a contact.
type ContactView struct {
	ID            uint
	Username      string
	Online        bool
	UnreadCount   int64
	LastMessageAt *time.Time
}

// getContactViews fetches the list of contacts with unread counts and sorting.
func getContactViews(userID uint) ([]ContactView, error) {
	var contactViews []ContactView

	// Updated query: LEFT JOIN messages, sort by newest message first.
	// We count unread messages sent FROM the contact TO the current user.
	err := database.DB.Table("users").
		Select("users.id, users.username, "+
			"COUNT(DISTINCT CASE WHEN m.receiver_id = ? AND m.sender_id = users.id AND m.is_read = false THEN m.id END) as unread_count, "+
			"MAX(m.created_at) as last_message_at", userID).
		Joins("LEFT JOIN messages m ON (m.sender_id = users.id AND m.receiver_id = ?) OR (m.sender_id = ? AND m.receiver_id = users.id)", userID, userID).
		Where("users.id != ?", userID).
		Group("users.id, users.username").
		Order("last_message_at DESC NULLS LAST, users.username ASC").
		Scan(&contactViews).Error

	if err != nil {
		return nil, err
	}

	// Update online status from WebSocket hub
	onlineIDs := ChatHub.OnlineUserIDs()
	for i := range contactViews {
		contactViews[i].Online = onlineIDs[contactViews[i].ID]
	}

	return contactViews, nil
}

// ShowChat renders the main layout with contact sidebar.
func ShowChat(c *fiber.Ctx) error {
	userID, username := getCurrentUser(c)
	if userID == 0 {
		return c.Redirect("/")
	}

	contactViews, err := getContactViews(userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Error loading contacts")
	}

	return c.Render("chat", fiber.Map{
		"UserID":   userID,
		"Username": username,
		"Contacts": contactViews,
	})
}

// GetContacts returns the HTMX sidebar partial.
func GetContacts(c *fiber.Ctx) error {
	userID, _ := getCurrentUser(c)
	if userID == 0 {
		return c.Status(fiber.StatusUnauthorized).SendString("Not logged in")
	}

	contactViews, err := getContactViews(userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Error loading contacts")
	}

	return c.Render("contacts", fiber.Map{
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

	// Mark messages from partner TO me as read
	result := database.DB.Table("messages").
		Where("receiver_id = ? AND sender_id = ? AND is_read = false", userID, uint(partnerID)).
		Update("is_read", true)

	if result.Error != nil {
		log.Printf("❌ Error marking messages as read: %v", result.Error)
	}

	// Trigger sidebar refresh for the current user via HTMX header
	c.Set("HX-Trigger", "refreshSidebar")

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
		"UserID":      userID,
		"PartnerID":   partner.ID,
		"PartnerName": partner.Username,
		"Messages":    messages,
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
	imageUrl := c.FormValue("image_url")
	if receiverIDStr == "" || (content == "" && imageUrl == "") {
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
		ImageUrl:   imageUrl,
		IsRead:     false,
		CreatedAt:  time.Now(),
	}
	database.DB.Create(&msg)

	// Load sender relation for rendering
	database.DB.Preload("Sender").First(&msg, msg.ID)

	// Send to receiver via WebSocket (with OOB for auto-append)
	receiverHTML := renderMessageHTMLOOB(msg, uint(receiverID))
	// Add a hidden trigger to refresh receiver's sidebar
	receiverHTML += `<div hx-swap-oob="afterbegin:body"><div hx-trigger="load" hx-get="/contacts" hx-target="#contact-list" hx-swap="innerHTML" class="hidden"></div></div>`
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
	rowClass := "flex-row"
	if isMine {
		bubbleClass = "message-mine"
		rowClass = "flex-row-reverse"
	}

	oobAttr := ""
	if oob {
		oobAttr = ` hx-swap-oob="beforeend:#messages"`
	}

	contentHTML := escapedContent
	if msg.ImageUrl != "" {
		escapedImg := html.EscapeString(msg.ImageUrl)
		imgTag := fmt.Sprintf(`<img src="%s" class="max-w-xs rounded-lg shadow-md my-2 object-contain"/>`, escapedImg)
		if escapedContent != "" {
			contentHTML = escapedContent + "<br>" + imgTag
		} else {
			contentHTML = imgTag
		}
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
		msg.ID, rowClass, oobAttr, bubbleClass, escapedUser, timeStr, contentHTML,
	)
}

// UploadImage handles image uploads, creating an instant message and broadcasting it.
func UploadImage(c *fiber.Ctx) error {
	userID, _ := getCurrentUser(c)
	if userID == 0 {
		return c.Status(fiber.StatusUnauthorized).SendString("Not logged in")
	}

	receiverIDStr := c.FormValue("receiver_id")
	if receiverIDStr == "" {
		return c.Status(fiber.StatusBadRequest).SendString("Missing receiver ID")
	}
	receiverID, err := strconv.ParseUint(receiverIDStr, 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid receiver ID")
	}

	file, err := c.FormFile("image")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Upload failed")
	}

	// Generate UUID filename
	ext := filepath.Ext(file.Filename)
	filename := uuid.New().String() + ext
	
	// Ensure directory exists
	if err := os.MkdirAll("./uploads", 0755); err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Error saving file")
	}

	savePath := "./uploads/" + filename
	if err := c.SaveFile(file, savePath); err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Error saving file")
	}

	imageUrl := "/uploads/" + filename

	// Save to database
	msg := models.Message{
		SenderID:   userID,
		ReceiverID: uint(receiverID),
		Content:    "Sent an image", // Optional descriptive text
		ImageUrl:   imageUrl,
		IsRead:     false,
		CreatedAt:  time.Now(),
	}
	database.DB.Create(&msg)

	// Load sender relation for rendering
	database.DB.Preload("Sender").First(&msg, msg.ID)

	// Broadcast to receiver via WebSocket
	receiverHTML := renderMessageHTMLOOB(msg, uint(receiverID))
	// Add trigger to refresh receiver's sidebar automatically
	receiverHTML += `<div hx-swap-oob="afterbegin:body"><div hx-trigger="load" hx-get="/contacts" hx-target="#contact-list" hx-swap="innerHTML" class="hidden"></div></div>`
	ChatHub.SendToUsers([]uint{uint(receiverID)}, []byte(receiverHTML))

	// Return sender's own bubble via HTMX response
	senderHTML := renderMessageHTML(msg, userID)
	c.Set("Content-Type", "text/html")
	return c.SendString(senderHTML)
}

