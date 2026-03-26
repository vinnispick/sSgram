package handlers_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ssgram/internal/database"
	"ssgram/internal/handlers"
	"ssgram/internal/models"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/html/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestApp() *fiber.App {
	// Initialize in-memory SQLite database
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		panic("Failed to connect to test database")
	}

	database.DB = db
	database.DB.AutoMigrate(&models.User{}, &models.Message{})

	// Setup template engine (pointing to actual views folder for testing)
	engine := html.New("../../views", ".html")

	// Create Fiber app
	app := fiber.New(fiber.Config{
		Views:       engine,
		ViewsLayout: "",
	})

	// Routes
	app.Get("/", handlers.ShowLogin)
	app.Post("/login", handlers.Login)
	app.Post("/logout", handlers.Logout)
	app.Get("/chat", func(c *fiber.Ctx) error {
		return c.SendString("Chat Page")
	})

	return app
}

func TestShowLogin(t *testing.T) {
	app := setupTestApp()

	// 1. Test unauthenticated request -> should return 200 OK (login page)
	req1 := httptest.NewRequest("GET", "/", nil)
	resp1, err := app.Test(req1)
	if err != nil {
		t.Fatalf("Failed to test request: %v", err)
	}
	if resp1.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 OK, got %v", resp1.StatusCode)
	}

	// 2. Test authenticated request -> should redirect to /chat
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.AddCookie(&http.Cookie{Name: "user_id", Value: "1"})
	resp2, err := app.Test(req2)
	if err != nil {
		t.Fatalf("Failed to test request: %v", err)
	}
	if resp2.StatusCode != http.StatusFound { // 302 Redirect
		t.Errorf("Expected status 302 Found, got %v", resp2.StatusCode)
	}
	if resp2.Header.Get("Location") != "/chat" {
		t.Errorf("Expected redirect location /chat, got %v", resp2.Header.Get("Location"))
	}
}

func createMultipartFormData(t *testing.T, fieldName, fieldValue string, fieldName2, fieldValue2 string) (string, io.Reader) {
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)
	
	if err := writer.WriteField(fieldName, fieldValue); err != nil {
		t.Fatalf("Failed to write field: %v", err)
	}
	if fieldName2 != "" {
		if err := writer.WriteField(fieldName2, fieldValue2); err != nil {
			t.Fatalf("Failed to write field: %v", err)
		}
	}
	
	writer.Close()
	return writer.FormDataContentType(), &b
}

func TestLogin(t *testing.T) {
	app := setupTestApp()

	t.Run("Empty Fields Validation", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/login", nil)
		// Empty body but HTMX header to get HTML error string
		req.Header.Set("HX-Request", "true")
		resp, _ := app.Test(req)
		
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK for HTMX error response, got %v", resp.StatusCode)
		}
		
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "Username and password required") {
			t.Errorf("Expected empty fields error message, got %s", string(body))
		}
	})

	t.Run("Username Length Validation", func(t *testing.T) {
		contentType, body := createMultipartFormData(t, "username", "a", "password", "123")
		req := httptest.NewRequest("POST", "/login", body)
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("HX-Request", "true")
		
		resp, _ := app.Test(req)
		respBody, _ := io.ReadAll(resp.Body)
		
		if !strings.Contains(string(respBody), "Username must be between 2 and 32 characters") {
			t.Errorf("Expected length validation error, got %s", string(respBody))
		}
	})

	t.Run("Successful Login and Registration", func(t *testing.T) {
		contentType, body := createMultipartFormData(t, "username", "testuser", "password", "password123")
		req := httptest.NewRequest("POST", "/login", body)
		req.Header.Set("Content-Type", contentType)
		// Simulating standard form post without HX-Request
		
		resp, _ := app.Test(req)
		
		if resp.StatusCode != http.StatusFound {
			t.Errorf("Expected status 302 Found, got %v", resp.StatusCode)
		}
		if resp.Header.Get("Location") != "/chat" {
			t.Errorf("Expected redirect to /chat, got %v", resp.Header.Get("Location"))
		}
		
		// Check cookies
		cookies := resp.Header.Values("Set-Cookie")
		hasUserId := false
		for _, cookie := range cookies {
			if strings.HasPrefix(cookie, "user_id=") {
				hasUserId = true
			}
		}
		if !hasUserId {
			t.Error("Expected user_id cookie to be set")
		}
	})

	t.Run("Invalid Password", func(t *testing.T) {
		// Attempt to login with same username but wrong password
		contentType, body := createMultipartFormData(t, "username", "testuser", "password", "wrongpass")
		req := httptest.NewRequest("POST", "/login", body)
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("HX-Request", "true")
		
		resp, _ := app.Test(req)
		
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK for HTMX error response, got %v", resp.StatusCode)
		}
		
		respBody, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(respBody), "Invalid password") {
			t.Errorf("Expected invalid password error, got %s", string(respBody))
		}
	})
}

func TestLogout(t *testing.T) {
	app := setupTestApp()

	req := httptest.NewRequest("POST", "/logout", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to test request: %v", err)
	}

	if resp.StatusCode != http.StatusFound { // 302 Redirect
		t.Errorf("Expected status 302 Found, got %v", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/" {
		t.Errorf("Expected redirect location /, got %v", resp.Header.Get("Location"))
	}

	// Verify cookies are cleared
	cookies := resp.Header.Values("Set-Cookie")
	if len(cookies) == 0 {
		t.Error("Expected Set-Cookie headers to clear cookies")
	}
	
	for _, cookie := range cookies {
		if strings.HasPrefix(cookie, "user_id=") || strings.HasPrefix(cookie, "username=") {
			cookieLower := strings.ToLower(cookie)
			if !strings.Contains(cookieLower, "max-age=0") && !strings.Contains(cookieLower, "expires=") {
				t.Errorf("Expected cookie to be cleared, got %s", cookie)
			}
		}
	}
}
