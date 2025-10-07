package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"TrinityBot/internal/storage"
)

// handleUpdate processes a single update from Telegram
func (b *Bot) handleUpdate(update tgbotapi.Update) {
	// Apply middleware (logging, authorization, etc.)
	if !b.applyMiddleware(update) {
		return
	}

	// Handle different types of updates
	if update.Message != nil {
		b.handleMessage(update.Message)
	} else if update.CallbackQuery != nil {
		b.handleCallbackQuery(update.CallbackQuery)
	} else if update.InlineQuery != nil {
		b.handleInlineQuery(update.InlineQuery)
	}
}

// handleMessage processes message updates
func (b *Bot) handleMessage(message *tgbotapi.Message) {
	// Handle commands
	if message.IsCommand() {
		b.handleCommand(message)
		return
	}

	// Create a draft post from message (text or photo+caption)
	var (
		postType string
		text     string
		photoID  *string
	)

	if message.Photo != nil && len(message.Photo) > 0 {
		// Pick highest resolution photo (last item)
		ps := message.Photo[len(message.Photo)-1]
		id := ps.FileID
		photoID = &id
		text = message.Caption
		postType = "photo"
	} else if message.Text != "" {
		text = message.Text
		postType = "text"
	} else {
		// Unsupported content type
		_, _ = b.SendReply(message.Chat.ID, message.MessageID, "Please send text or a photo with optional caption.")
		return
	}

	ctx, cancel := b.dbCtx()
	defer cancel()
	id, err := b.repo.CreatePost(ctx, &storage.Post{
		TelegramUserID: message.From.ID,
		ChatID:         message.Chat.ID,
		MessageID:      message.MessageID,
		Type:           postType,
		TextContent:    text,
		PhotoFileID:    photoID,
	})
	if err != nil {
		log.Printf("create post error: %v", err)
		_, _ = b.SendReply(message.Chat.ID, message.MessageID, "Error creating draft. Please try again.")
		return
	}

	// Send platform selection UI
	markup, err := b.buildTargetsMarkup(ctx, id)
	if err != nil {
		log.Printf("build markup error: %v", err)
	}
	textTitle := fmt.Sprintf("Draft created (#%d). Select platforms and press Publish.", id)
	msg := tgbotapi.NewMessage(message.Chat.ID, textTitle)
	msg.ReplyMarkup = markup
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("send draft msg: %v", err)
	}
}

// handleCommand processes command messages
func (b *Bot) handleCommand(message *tgbotapi.Message) {
	command := message.Command()
	args := message.CommandArguments()

	log.Printf("Received command from %s: %s %s", message.From.UserName, command, args)

	switch strings.ToLower(command) {
	case "start":
		b.handleStartCommand(message)
	case "help":
		b.handleHelpCommand(message)
	default:
		_, err := b.SendReply(message.Chat.ID, message.MessageID, "Unknown command. Try /help")
		if err != nil {
			log.Printf("Error sending message: %v", err)
		}
	}
}

// handleCallbackQuery processes callback query updates (inline keyboard buttons)
func (b *Bot) handleCallbackQuery(query *tgbotapi.CallbackQuery) {
	log.Printf("Received callback from %s: %s", query.From.UserName, query.Data)

	// Expect formats:
	// tgl:<postID>:<platform>
	// pub:<postID>
	// can:<postID>
	parts := strings.Split(query.Data, ":")
	if len(parts) < 2 {
		_, _ = b.api.Request(tgbotapi.NewCallback(query.ID, "Invalid action"))
		return
	}

	action := parts[0]
	postID64, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		_, _ = b.api.Request(tgbotapi.NewCallback(query.ID, "Invalid post id"))
		return
	}

	switch action {
	case "tgl":
		if len(parts) != 3 {
			_, _ = b.api.Request(tgbotapi.NewCallback(query.ID, "Invalid toggle"))
			return
		}
		platform := parts[2]
		ctx, cancel := b.dbCtx()
		defer cancel()
		enabled, err := b.repo.ToggleTarget(ctx, postID64, platform)
		if err != nil {
			log.Printf("toggle error: %v", err)
			_, _ = b.api.Request(tgbotapi.NewCallback(query.ID, "Error"))
			return
		}
		// Update markup to reflect toggle
		markup, err := b.buildTargetsMarkup(ctx, postID64)
		if err == nil {
			edit := tgbotapi.NewEditMessageReplyMarkup(query.Message.Chat.ID, query.Message.MessageID, markup)
			if _, err := b.api.Request(edit); err != nil {
				log.Printf("edit markup: %v", err)
			}
		}
		label := "disabled"
		if enabled {
			label = "enabled"
		}
		_, _ = b.api.Request(tgbotapi.NewCallback(query.ID, fmt.Sprintf("%s %s", platform, label)))
	case "pub":
		ctx, cancel := b.dbCtx()
		defer cancel()
		if err := b.repo.SetPostStatus(ctx, postID64, "queued"); err != nil {
			log.Printf("queue error: %v", err)
			_, _ = b.api.Request(tgbotapi.NewCallback(query.ID, "Error"))
			return
		}
		_, _ = b.api.Request(tgbotapi.NewCallback(query.ID, "Queued"))
		msg := tgbotapi.NewMessage(query.Message.Chat.ID, fmt.Sprintf("Post #%d queued for publishing. Integrations coming soon.", postID64))
		_, _ = b.api.Send(msg)
	case "can":
		ctx, cancel := b.dbCtx()
		defer cancel()
		if err := b.repo.SetPostStatus(ctx, postID64, "canceled"); err != nil {
			log.Printf("cancel error: %v", err)
			_, _ = b.api.Request(tgbotapi.NewCallback(query.ID, "Error"))
			return
		}
		_, _ = b.api.Request(tgbotapi.NewCallback(query.ID, "Canceled"))
		msg := tgbotapi.NewMessage(query.Message.Chat.ID, fmt.Sprintf("Post #%d canceled.", postID64))
		_, _ = b.api.Send(msg)
	default:
		_, _ = b.api.Request(tgbotapi.NewCallback(query.ID, "Unknown action"))
	}
}

// handleInlineQuery processes inline query updates
func (b *Bot) handleInlineQuery(query *tgbotapi.InlineQuery) {
	log.Printf("Received inline query from %s: %s", query.From.UserName, query.Query)

	// Create some results (replace with your logic)
	results := []interface{}{
		tgbotapi.NewInlineQueryResultArticle(
			"1",
			"Example Result",
			"This is an example result",
		),
	}

	// Answer the inline query
	inlineResponse := tgbotapi.InlineConfig{
		InlineQueryID: query.ID,
		Results:       results,
		CacheTime:     60,
	}

	_, err := b.api.Request(inlineResponse)
	if err != nil {
		log.Printf("Error answering inline query: %v", err)
	}
}

// Command handlers

func (b *Bot) handleStartCommand(message *tgbotapi.Message) {
	welcomeText := "Welcome to the bot! Type /help to see available commands."
	_, err := b.SendMessage(message.Chat.ID, welcomeText)
	if err != nil {
		log.Printf("Error sending message: %v", err)
	}
}

func (b *Bot) handleHelpCommand(message *tgbotapi.Message) {
	helpText := `
Available commands:
/start - Start the bot
/help - Show this help message
Send a text message or a photo with caption to create a draft post.
Use the buttons to select platforms and publish.
`
	_, err := b.SendMessage(message.Chat.ID, helpText)
	if err != nil {
		log.Printf("Error sending message: %v", err)
	}
}

// buildTargetsMarkup builds inline keyboard with platform toggles and actions
func (b *Bot) buildTargetsMarkup(ctx context.Context, postID int64) (tgbotapi.InlineKeyboardMarkup, error) {
	// Caller provides context (usually from b.dbCtx)
	selected, err := b.repo.ListTargets(ctx, postID)
	if err != nil {
		return tgbotapi.InlineKeyboardMarkup{}, err
	}

	btn := func(name, key string) tgbotapi.InlineKeyboardButton {
		label := name
		if selected[key] {
			label = "✅ " + name
		}
		return tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("tgl:%d:%s", postID, key))
	}

	row1 := tgbotapi.NewInlineKeyboardRow(
		btn("Twitter", "twitter"),
		btn("Pinterest", "pinterest"),
		btn("Facebook", "facebook"),
	)
	row2 := tgbotapi.NewInlineKeyboardRow(
		btn("Instagram", "instagram"),
		btn("TikTok", "tiktok"),
	)
	actions := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🚀 Publish", fmt.Sprintf("pub:%d", postID)),
		tgbotapi.NewInlineKeyboardButtonData("✖️ Cancel", fmt.Sprintf("can:%d", postID)),
	)

	return tgbotapi.NewInlineKeyboardMarkup(row1, row2, actions), nil
}
