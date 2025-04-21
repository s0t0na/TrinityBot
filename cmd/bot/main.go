package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"TrinityBot/internal/bot"
	"TrinityBot/internal/config"
)

func main() {
	// Initialize configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize bot
	telegramBot, err := bot.New(cfg)
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
