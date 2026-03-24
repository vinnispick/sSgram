package handlers

import (
	"fmt"
	"time"

	"ssgram/internal/database"
	"ssgram/internal/models"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
)

// ShowLogin renders the login page or redirects to chat if already logged in.
func ShowLogin(c *fiber.Ctx) error {
	if c.Cookies("user_id") != "" {
		return c.Redirect("/chat")
	}
	return c.Render("index", fiber.Map{})
}

// Login finds or verifies the user and sets session cookies.
func Login(c *fiber.Ctx) error {
	username := c.FormValue("username")
	password := c.FormValue("password")

	if username == "" || password == "" {
		if c.Get("HX-Request") != "" {
			return c.Status(fiber.StatusBadRequest).SendString("Username and password required")
		}
		return c.Redirect("/")
	}

	var user models.User
	result := database.DB.Where("username = ?", username).First(&user)

	if result.Error == nil {
		// User exists, check password
		err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
		if err != nil {
			if c.Get("HX-Request") != "" {
				return c.SendString(`<div id="error-message" class="bg-red-500/10 border border-red-500/50 text-red-500 p-3 rounded-lg text-sm mb-4 animate-pulse">Invalid password</div>`)
			}
			return c.Status(fiber.StatusUnauthorized).SendString("Invalid password")
		}
	} else {
		// User doesn't exist, create new
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("Error securing password")
		}

		user = models.User{
			Username:     username,
			PasswordHash: string(hash),
		}
		if err := database.DB.Create(&user).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("Error creating user")
		}
	}

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

	if c.Get("HX-Request") != "" {
		c.Set("HX-Redirect", "/chat")
		return c.SendStatus(fiber.StatusOK)
	}
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
