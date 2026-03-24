package models

import "time"

// Message represents a chat message stored in the database.
type Message struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	Username  string    `gorm:"size:64;not null" json:"username"`
	Content   string    `gorm:"type:text;not null" json:"content"`
	CreatedAt time.Time `json:"created_at"`
}
