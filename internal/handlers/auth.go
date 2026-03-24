package handlers

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

// ShowLogin renders the login page or redirects to chat if already logged in.
func ShowLogin(c *fiber.Ctx) error {
	username := c.Cookies("username")
	if username != "" {
		return c.Redirect("/chat")
	}
	return c.Render("index", fiber.Map{})
}

// Login sets username cookie and redirects to chat.
func Login(c *fiber.Ctx) error {
	username := c.FormValue("username")
	if username == "" {
		return c.Redirect("/")
	}

	c.Cookie(&fiber.Cookie{
		Name:     "username",
		Value:    username,
		Expires:  time.Now().Add(24 * 7 * time.Hour),
		HTTPOnly: true,
		SameSite: "Lax",
	})

	return c.Redirect("/chat")
}

// Logout clears the username cookie and redirects to login.
func Logout(c *fiber.Ctx) error {
	c.Cookie(&fiber.Cookie{
		Name:     "username",
		Value:    "",
		Expires:  time.Now().Add(-1 * time.Hour),
		HTTPOnly: true,
		SameSite: "Lax",
	})

	return c.Redirect("/")
}
