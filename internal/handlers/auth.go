package handlers

import (
	"fmt"
	"time"

	"ssgram/internal/database"
	"ssgram/internal/models"

	"github.com/gofiber/fiber/v2"
)

// ShowLogin renders the login page or redirects to chat if already logged in.
func ShowLogin(c *fiber.Ctx) error {
	if c.Cookies("user_id") != "" {
		return c.Redirect("/chat")
	}
	return c.Render("index", fiber.Map{})
}

// Login finds or creates the user and sets session cookies.
func Login(c *fiber.Ctx) error {
	username := c.FormValue("username")
	if username == "" {
		return c.Redirect("/")
	}

	// FirstOrCreate — find existing user or create new one
	var user models.User
	database.DB.Where("username = ?", username).FirstOrCreate(&user, models.User{
		Username: username,
	})

	// Set cookies
	expiry := time.Now().Add(24 * 7 * time.Hour)

	c.Cookie(&fiber.Cookie{
		Name:     "user_id",
		Value:    fmt.Sprintf("%d", user.ID),
		Expires:  expiry,
		HTTPOnly: true,
		SameSite: "Lax",
	})
	c.Cookie(&fiber.Cookie{
		Name:     "username",
		Value:    user.Username,
		Expires:  expiry,
		HTTPOnly: true,
		SameSite: "Lax",
	})

	return c.Redirect("/chat")
}

// Logout clears session cookies and redirects to login.
func Logout(c *fiber.Ctx) error {
	expired := time.Now().Add(-1 * time.Hour)

	c.Cookie(&fiber.Cookie{
		Name: "user_id", Value: "", Expires: expired,
		HTTPOnly: true, SameSite: "Lax",
	})
	c.Cookie(&fiber.Cookie{
		Name: "username", Value: "", Expires: expired,
		HTTPOnly: true, SameSite: "Lax",
	})

	return c.Redirect("/")
}
