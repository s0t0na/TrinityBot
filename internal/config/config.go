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

	// Database configuration
	DatabaseURL string // Prefer single DATABASE_URL
	DBHost      string
	DBPort      string
	DBUser      string
	DBPassword  string
	DBName      string
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

	// Optional: Allowed users (comma-separated int64 list)
	if allowed := os.Getenv("ALLOWED_USERS"); allowed != "" {
		var ids []int64
		var cur int64
		var neg bool
		// Simple fast parser for digits, commas, optional spaces and leading '-'
		for i := 0; i < len(allowed); i++ {
			c := allowed[i]
			switch {
			case c >= '0' && c <= '9':
				cur = cur*10 + int64(c-'0')
			case c == '-':
				if cur == 0 {
					neg = true
				}
			default:
				if neg {
					cur = -cur
				}
				if cur != 0 {
					ids = append(ids, cur)
				}
				cur = 0
				neg = false
			}
		}
		if neg {
			cur = -cur
		}
		if cur != 0 {
			ids = append(ids, cur)
		}
		config.AllowedUsers = ids
	}

	// Database configuration
	config.DatabaseURL = os.Getenv("DATABASE_URL")
	config.DBHost = os.Getenv("POSTGRES_HOST")
	if config.DBHost == "" {
		config.DBHost = os.Getenv("DB_HOST")
	}
	config.DBPort = os.Getenv("POSTGRES_PORT")
	if config.DBPort == "" {
		config.DBPort = os.Getenv("DB_PORT")
	}
	config.DBUser = os.Getenv("POSTGRES_USER")
	if config.DBUser == "" {
		config.DBUser = os.Getenv("DB_USER")
	}
	config.DBPassword = os.Getenv("POSTGRES_PASSWORD")
	if config.DBPassword == "" {
		config.DBPassword = os.Getenv("DB_PASSWORD")
	}
	config.DBName = os.Getenv("POSTGRES_DB")
	if config.DBName == "" {
		config.DBName = os.Getenv("DB_NAME")
	}

	return config, nil
}
