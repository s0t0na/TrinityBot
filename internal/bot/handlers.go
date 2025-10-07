package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"trinity_bot/internal/connectors/pinterest"
	"trinity_bot/internal/connectors/twitter"
	"trinity_bot/internal/storage"
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
		slog.Error("Create post error", "err", err)
		_, _ = b.SendReply(message.Chat.ID, message.MessageID, "Error creating draft. Please try again.")
		return
	}

	// Send platform selection UI
	markup, err := b.buildTargetsMarkup(ctx, id)
	if err != nil {
		slog.Error("Build markup error", "err", err)
	}
	textTitle := fmt.Sprintf("Draft created (#%d). Select platforms and press Publish.", id)
	msg := tgbotapi.NewMessage(message.Chat.ID, textTitle)
	msg.ReplyMarkup = markup
	if _, err := b.api.Send(msg); err != nil {
		slog.Error("Send draft message error", "err", err)
	}
}

// handleCommand processes command messages
func (b *Bot) handleCommand(message *tgbotapi.Message) {
	command := message.Command()
	args := message.CommandArguments()

	slog.Info("Command received", "username", message.From.UserName, "user_id", message.From.ID, "command", command, "args", args)

	switch strings.ToLower(command) {
	case "start":
		b.handleStartCommand(message)
	case "help":
		b.handleHelpCommand(message)
	default:
		_, err := b.SendReply(message.Chat.ID, message.MessageID, "Unknown command. Try /help")
		if err != nil {
			slog.Error("Send unknown command reply error", "err", err)
		}
	}
}

// handleCallbackQuery processes callback query updates (inline keyboard buttons)
func (b *Bot) handleCallbackQuery(query *tgbotapi.CallbackQuery) {
	slog.Info("Callback received", "username", query.From.UserName, "user_id", query.From.ID, "data", query.Data)

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
			slog.Error("Toggle target error", "err", err, "post_id", postID64, "platform", platform)
			_, _ = b.api.Request(tgbotapi.NewCallback(query.ID, "Error"))
			return
		}
		// Update markup to reflect toggle
		markup, err := b.buildTargetsMarkup(ctx, postID64)
		if err == nil {
			edit := tgbotapi.NewEditMessageReplyMarkup(query.Message.Chat.ID, query.Message.MessageID, markup)
			if _, err := b.api.Request(edit); err != nil {
				slog.Warn("Edit markup failed", "err", err)
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
			slog.Error("Queue post error", "err", err, "post_id", postID64)
			_, _ = b.api.Request(tgbotapi.NewCallback(query.ID, "Error"))
			return
		}
		_, _ = b.api.Request(tgbotapi.NewCallback(query.ID, "Queued"))
		// Attempt immediate publish for selected platforms (Twitter only for now)
		if err := b.publishSelected(ctx, postID64); err != nil {
			slog.Error("Publish selected error", "err", err, "post_id", postID64)
			_, _ = b.api.Send(tgbotapi.NewMessage(query.Message.Chat.ID, fmt.Sprintf("Post #%d queued, publish attempt failed: %v", postID64, err)))
		} else {
			_, _ = b.api.Send(tgbotapi.NewMessage(query.Message.Chat.ID, fmt.Sprintf("Post #%d published to selected platforms (where applicable).", postID64)))
		}
	case "can":
		ctx, cancel := b.dbCtx()
		defer cancel()
		if err := b.repo.SetPostStatus(ctx, postID64, "canceled"); err != nil {
			slog.Error("Cancel post error", "err", err, "post_id", postID64)
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
	slog.Info("Inline query", "username", query.From.UserName, "user_id", query.From.ID, "query", query.Query)

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
		slog.Error("Answer inline query error", "err", err)
	}
}

// Command handlers

func (b *Bot) handleStartCommand(message *tgbotapi.Message) {
	welcomeText := "Welcome to the bot! Type /help to see available commands."
	_, err := b.SendMessage(message.Chat.ID, welcomeText)
	if err != nil {
		slog.Error("Send welcome error", "err", err)
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
		slog.Error("Send help error", "err", err)
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

// publishSelected publishes the post to all currently selected targets.
func (b *Bot) publishSelected(ctx context.Context, postID int64) error {
	selections, err := b.repo.ListTargets(ctx, postID)
	if err != nil {
		return err
	}
	post, err := b.repo.GetPost(ctx, postID)
	if err != nil {
		return err
	}
	if selections["twitter"] {
		if err := b.publishToTwitter(ctx, post); err != nil {
			return err
		}
	}
	if selections["pinterest"] {
		if err := b.publishToPinterest(ctx, post); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) publishToTwitter(ctx context.Context, p *storage.Post) error {
	if b.config.TwitterConsumerKey == "" || b.config.TwitterConsumerSecret == "" || b.config.TwitterAccessToken == "" || b.config.TwitterAccessSecret == "" {
		msg := "Twitter credentials missing"
		_ = b.repo.SetTargetStatus(ctx, p.ID, "twitter", "failed", nil, &msg)
		_ = b.repo.AddLog(ctx, p.ID, ptr("twitter"), "error", msg)
		return fmt.Errorf(msg)
	}
	twc, err := twitter.New(twitter.Credentials{
		ConsumerKey:    b.config.TwitterConsumerKey,
		ConsumerSecret: b.config.TwitterConsumerSecret,
		AccessToken:    b.config.TwitterAccessToken,
		AccessSecret:   b.config.TwitterAccessSecret,
	})
	if err != nil {
		_ = b.repo.SetTargetStatus(ctx, p.ID, "twitter", "failed", nil, strptr(err.Error()))
		_ = b.repo.AddLog(ctx, p.ID, ptr("twitter"), "error", err.Error())
		return err
	}

	var media [][]byte
	var mediaTypes []string
	if p.PhotoFileID != nil {
		data, ctype, err := b.downloadTelegramFile(ctx, *p.PhotoFileID)
		if err != nil {
			_ = b.repo.SetTargetStatus(ctx, p.ID, "twitter", "failed", nil, strptr(err.Error()))
			_ = b.repo.AddLog(ctx, p.ID, ptr("twitter"), "error", "telegram download: "+err.Error())
			return err
		}
		if ctype == "application/octet-stream" || ctype == "" {
			ctype = "image/jpeg"
		}
		media = append(media, data)
		mediaTypes = append(mediaTypes, ctype)
	}
	tweetID, err := twc.Publish(ctx, p.TextContent, media, mediaTypes)
	if err != nil {
		_ = b.repo.SetTargetStatus(ctx, p.ID, "twitter", "failed", nil, strptr(err.Error()))
		_ = b.repo.AddLog(ctx, p.ID, ptr("twitter"), "error", err.Error())
		return err
	}
	_ = b.repo.SetTargetStatus(ctx, p.ID, "twitter", "published", &tweetID, nil)
	_ = b.repo.AddLog(ctx, p.ID, ptr("twitter"), "published", "tweet_id="+tweetID)
	return nil
}

func (b *Bot) publishToPinterest(ctx context.Context, p *storage.Post) error {
	if b.config.PinterestAccessToken == "" || b.config.PinterestBoardID == "" {
		msg := "Pinterest token or board ID missing"
		_ = b.repo.SetTargetStatus(ctx, p.ID, "pinterest", "failed", nil, &msg)
		_ = b.repo.AddLog(ctx, p.ID, ptr("pinterest"), "error", msg)
		return fmt.Errorf(msg)
	}
	if p.PhotoFileID == nil {
		msg := "Pinterest requires an image"
		_ = b.repo.SetTargetStatus(ctx, p.ID, "pinterest", "failed", nil, &msg)
		_ = b.repo.AddLog(ctx, p.ID, ptr("pinterest"), "error", msg)
		return fmt.Errorf(msg)
	}
	// Download image from Telegram
	data, ctype, err := b.downloadTelegramFile(ctx, *p.PhotoFileID)
	if err != nil {
		_ = b.repo.SetTargetStatus(ctx, p.ID, "pinterest", "failed", nil, strptr(err.Error()))
		_ = b.repo.AddLog(ctx, p.ID, ptr("pinterest"), "error", "telegram download: "+err.Error())
		return err
	}
	if ctype == "" {
		ctype = "image/jpeg"
	}
	// Create client and pin
	cli, err := pinterest.New(pinterest.Credentials{AccessToken: b.config.PinterestAccessToken})
	if err != nil {
		_ = b.repo.SetTargetStatus(ctx, p.ID, "pinterest", "failed", nil, strptr(err.Error()))
		_ = b.repo.AddLog(ctx, p.ID, ptr("pinterest"), "error", err.Error())
		return err
	}
	// Pinterest recommends short title; use truncated text if present
	title := p.TextContent
	if len(title) > 100 {
		title = title[:100]
	}
	pinID, err := cli.CreatePin(ctx, b.config.PinterestBoardID, title, p.TextContent, "", data, ctype)
	if err != nil {
		_ = b.repo.SetTargetStatus(ctx, p.ID, "pinterest", "failed", nil, strptr(err.Error()))
		_ = b.repo.AddLog(ctx, p.ID, ptr("pinterest"), "error", err.Error())
		return err
	}
	_ = b.repo.SetTargetStatus(ctx, p.ID, "pinterest", "published", &pinID, nil)
	_ = b.repo.AddLog(ctx, p.ID, ptr("pinterest"), "published", "pin_id="+pinID)
	return nil
}

func ptr[T any](v T) *T       { return &v }
func strptr(s string) *string { return &s }
