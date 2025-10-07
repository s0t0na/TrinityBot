package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"trinity_bot/internal/bot"
	"trinity_bot/internal/config"
	"trinity_bot/internal/db"
	"trinity_bot/internal/storage"
)

func main() {
	// Temporary default logger (JSON at info) until we load config
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{})))

	// Initialize configuration
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load configuration", "err", err)
		os.Exit(1)
	}

	// Configure default logger level based on DEBUG_MODE
	level := slog.LevelInfo
	if cfg.DebugMode {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})).With("service", "trinitybot"))

	// Connect DB and run migrations
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sqlDB, err := db.Connect(ctx, cfg)
	if err != nil {
		slog.Error("Failed to connect to DB", "err", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	// Repository
	repo := storage.New(sqlDB)

	// Initialize bot
	telegramBot, err := bot.New(cfg, repo)
	if err != nil {
		slog.Error("Failed to initialize bot", "err", err)
		os.Exit(1)
	}

	// Start bot in a separate goroutine
	go func() {
		slog.Info("Starting Telegram bot")
		if err := telegramBot.Start(); err != nil {
			slog.Error("Error running bot", "err", err)
			os.Exit(1)
		}
	}()

	// Setup graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down bot")
	telegramBot.Stop()
	slog.Info("Bot gracefully stopped")
}
