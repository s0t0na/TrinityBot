package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"TrinityBot/internal/bot"
	"TrinityBot/internal/config"
	"TrinityBot/internal/db"
	"TrinityBot/internal/storage"
)

func main() {
	// Initialize configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Connect DB and run migrations
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sqlDB, err := db.Connect(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer sqlDB.Close()

	// Repository
	repo := storage.New(sqlDB)

	// Initialize bot
	telegramBot, err := bot.New(cfg, repo)
	if err != nil {
		log.Fatalf("Failed to initialize bot: %v", err)
	}

	// Start bot in a separate goroutine
	go func() {
		log.Println("Starting Telegram bot...")
		if err := telegramBot.Start(); err != nil {
			log.Fatalf("Error running bot: %v", err)
		}
	}()

	// Setup graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down bot...")
	telegramBot.Stop()
	log.Println("Bot gracefully stopped")
}
