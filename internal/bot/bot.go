package bot

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"trinity_bot/internal/config"
	"trinity_bot/internal/storage"
)

// Bot represents the Telegram bot
type Bot struct {
	api      *tgbotapi.BotAPI
	config   *config.Config
	updates  tgbotapi.UpdatesChannel
	server   *http.Server // For webhook mode
	stopChan chan struct{}
	repo     storage.PostRepository

	mu       sync.Mutex
	sessions map[int64]*PostSession // key: chatID
}

// New creates a new bot instance
func New(cfg *config.Config, repo storage.PostRepository) (*Bot, error) {
	// Initialize Telegram API
	api, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create Telegram bot: %w", err)
	}

	// Disable tgbotapi internal debug to keep logs JSON-only via slog
	api.Debug = false

	// Create a bot instance
	bot := &Bot{
		api:      api,
		config:   cfg,
		stopChan: make(chan struct{}),
		repo:     repo,
		sessions: make(map[int64]*PostSession),
	}

	slog.Info("Authorized on Telegram", "username", api.Self.UserName)
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
	// Ensure webhook is removed when using long polling to avoid 409 conflicts
	if _, err := b.api.Request(tgbotapi.DeleteWebhookConfig{DropPendingUpdates: false}); err != nil {
		slog.Warn("Failed to delete webhook before polling", "err", err)
	}

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
		slog.Warn("Telegram webhook error", "message", info.LastErrorMessage, "timestamp", info.LastErrorDate)
	}

	updates := b.api.ListenForWebhook("/" + b.api.Token)
	b.updates = updates

	// Start webhook server
	go func() {
		slog.Info("Starting webhook server", "port", b.config.WebhookPort)
		if err := http.ListenAndServe(":"+b.config.WebhookPort, nil); err != nil {
			slog.Error("Webhook server error", "err", err)
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

// helper: context with timeout for DB ops
func (b *Bot) dbCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 3*time.Second)
}

// downloadTelegramFile downloads a file by its Telegram FileID and returns bytes and content-type.
func (b *Bot) downloadTelegramFile(ctx context.Context, fileID string) ([]byte, string, error) {
	f, err := b.api.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return nil, "", fmt.Errorf("get file: %w", err)
	}
	if f.FilePath == "" {
		return nil, "", fmt.Errorf("empty file path for fileID %s", fileID)
	}
	raw := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", url.PathEscape(b.api.Token), f.FilePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("telegram file download status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	ctype := resp.Header.Get("Content-Type")
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	return data, ctype, nil
}

// getTelegramFileURL returns a publicly accessible URL for a Telegram fileID.
func (b *Bot) getTelegramFileURL(fileID string) (string, error) {
	f, err := b.api.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return "", fmt.Errorf("get file: %w", err)
	}
	if f.FilePath == "" {
		return "", fmt.Errorf("empty file path for fileID %s", fileID)
	}
	raw := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", url.PathEscape(b.api.Token), f.FilePath)
	return raw, nil
}

type PostSession struct {
	PostID     int64
	Step       string // compose | confirm
	MediaCount int
}

func (b *Bot) setSession(chatID int64, s *PostSession) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sessions[chatID] = s
}

func (b *Bot) getSession(chatID int64) (*PostSession, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.sessions[chatID]
	return s, ok
}

func (b *Bot) clearSession(chatID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sessions, chatID)
}
