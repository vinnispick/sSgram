package models

import "time"

// User represents a registered user.
type User struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	Username  string    `gorm:"size:64;uniqueIndex;not null" json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

// Message represents a private chat message between two users.
type Message struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	SenderID   uint      `gorm:"not null;index" json:"sender_id"`
	ReceiverID uint      `gorm:"not null;index" json:"receiver_id"`
	Content    string    `gorm:"type:text;not null" json:"content"`
	CreatedAt  time.Time `json:"created_at"`

	Sender   User `gorm:"foreignKey:SenderID" json:"sender"`
	Receiver User `gorm:"foreignKey:ReceiverID" json:"receiver"`
}
