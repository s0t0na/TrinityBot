package config

import (
	"errors"
	"os"

	"github.com/joho/godotenv"
)

// Config stores the application configuration
type Config struct {
	TelegramToken string
	DebugMode     bool
	UpdateTimeout int
	AllowedUsers  []int64 // Optional: List of allowed user IDs
	WebhookURL    string  // Optional: for webhook mode instead of polling
	WebhookPort   string  // Optional: port for webhook server
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	// Load .env file if it exists
	_ = godotenv.Load()

	// Get Telegram token
	token := os.Getenv("TELEGRAM_TOKEN")
	if token == "" {
		return nil, errors.New("TELEGRAM_TOKEN environment variable is required")
	}

	// Create config with defaults
	config := &Config{
		TelegramToken: token,
		DebugMode:     os.Getenv("DEBUG_MODE") == "true",
		UpdateTimeout: 60, // Default timeout in seconds
	}

	// Optional: Set webhook URL if provided
	webhookURL := os.Getenv("WEBHOOK_URL")
	if webhookURL != "" {
		config.WebhookURL = webhookURL

		// If webhook is used, get port
		port := os.Getenv("PORT")
		if port == "" {
			port = "8443" // Default webhook port
		}
		config.WebhookPort = port
	}

	return config, nil
}
