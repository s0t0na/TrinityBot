package bot

import (
	"fmt"
	"log"
	"net/http"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"TrinityBot/internal/config"
)

// Bot represents the Telegram bot
type Bot struct {
	api      *tgbotapi.BotAPI
	config   *config.Config
	updates  tgbotapi.UpdatesChannel
	server   *http.Server // For webhook mode
	stopChan chan struct{}
}

// New creates a new bot instance
func New(cfg *config.Config) (*Bot, error) {
	// Initialize Telegram API
	api, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create Telegram bot: %w", err)
	}

	// Set debugging mode
	api.Debug = cfg.DebugMode

	// Create a bot instance
	bot := &Bot{
		api:      api,
		config:   cfg,
		stopChan: make(chan struct{}),
	}

	log.Printf("Authorized on account %s", api.Self.UserName)
	return bot, nil
}

// Start starts the bot (either with webhook or long polling)
func (b *Bot) Start() error {
	if b.config.WebhookURL != "" {
		return b.startWebhook()
	}
	return b.startLongPolling()
}

// startLongPolling starts the bot with long polling
func (b *Bot) startLongPolling() error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = b.config.UpdateTimeout

	updates := b.api.GetUpdatesChan(u)
	b.updates = updates

	for {
		select {
		case update := <-updates:
			go b.handleUpdate(update)
		case <-b.stopChan:
			return nil
		}
	}
}

// startWebhook starts the bot with webhook
func (b *Bot) startWebhook() error {
	wh, err := tgbotapi.NewWebhook(b.config.WebhookURL + "/" + b.api.Token)
	if err != nil {
		return fmt.Errorf("failed to create webhook: %w", err)
	}

	_, err = b.api.Request(wh)
	if err != nil {
		return fmt.Errorf("failed to set webhook: %w", err)
	}

	info, err := b.api.GetWebhookInfo()
	if err != nil {
		return fmt.Errorf("failed to get webhook info: %w", err)
	}

	if info.LastErrorDate != 0 {
		log.Printf("Webhook error: %s", info.LastErrorMessage)
	}

	updates := b.api.ListenForWebhook("/" + b.api.Token)
	b.updates = updates

	// Start webhook server
	go func() {
		log.Printf("Starting webhook server on port %s", b.config.WebhookPort)
		if err := http.ListenAndServe(":"+b.config.WebhookPort, nil); err != nil {
			log.Printf("ListenAndServe error: %v", err)
		}
	}()

	for {
		select {
		case update := <-updates:
			go b.handleUpdate(update)
		case <-b.stopChan:
			return nil
		}
	}
}

// Stop stops the bot
func (b *Bot) Stop() {
	if b.config.WebhookURL != "" {
		// Remove webhook
		_, _ = b.api.Request(tgbotapi.DeleteWebhookConfig{})
		if b.server != nil {
			_ = b.server.Close()
		}
	}
	close(b.stopChan)
}

// SendMessage sends a message to the given chat
func (b *Bot) SendMessage(chatID int64, text string) (tgbotapi.Message, error) {
	msg := tgbotapi.NewMessage(chatID, text)
	return b.api.Send(msg)
}

// SendReply sends a reply to a message
func (b *Bot) SendReply(chatID int64, messageID int, text string) (tgbotapi.Message, error) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyToMessageID = messageID
	return b.api.Send(msg)
}
