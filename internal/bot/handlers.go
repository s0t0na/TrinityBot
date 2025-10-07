package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"trinity_bot/internal/connectors/facebook"
	"trinity_bot/internal/connectors/instagram"
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

	// If in a /post session, consume input for the current step
	if s, ok := b.getSession(message.Chat.ID); ok {
		switch s.Step {
		case "compose":
			ctx, cancel := b.dbCtx()
			defer cancel()
			contentAdded := false
			// Photo (with optional caption)
			if message.Photo != nil && len(message.Photo) > 0 {
				cnt, _ := b.repo.CountMedia(ctx, s.PostID)
				if cnt < 10 {
					ps := message.Photo[len(message.Photo)-1]
					if _, err := b.repo.AddMedia(ctx, s.PostID, ps.FileID, "photo"); err == nil {
						cnt++
						_, _ = b.SendReply(message.Chat.ID, message.MessageID, fmt.Sprintf("Photo added (%d/10).", cnt))
						contentAdded = true
						if cap := strings.TrimSpace(message.Caption); cap != "" {
							_ = b.repo.AppendPostText(ctx, s.PostID, cap)
						}
					} else {
						slog.Error("add media error", "err", err)
					}
				} else {
					_, _ = b.SendReply(message.Chat.ID, message.MessageID, "You already added 10 items.")
				}
			}
			// Video (with optional caption)
			if message.Video != nil {
				cnt, _ := b.repo.CountMedia(ctx, s.PostID)
				if cnt < 10 {
					if _, err := b.repo.AddMedia(ctx, s.PostID, message.Video.FileID, "video"); err == nil {
						cnt++
						_, _ = b.SendReply(message.Chat.ID, message.MessageID, fmt.Sprintf("Video added (%d/10).", cnt))
						contentAdded = true
						if cap := strings.TrimSpace(message.Caption); cap != "" {
							_ = b.repo.AppendPostText(ctx, s.PostID, cap)
						}
					} else {
						slog.Error("add media error", "err", err)
					}
				} else {
					_, _ = b.SendReply(message.Chat.ID, message.MessageID, "You already added 10 items.")
				}
			}
			// Text (append)
			if t := strings.TrimSpace(message.Text); t != "" {
				if err := b.repo.AppendPostText(ctx, s.PostID, t); err != nil {
					slog.Error("append post text error", "err", err)
				} else {
					_, _ = b.SendReply(message.Chat.ID, message.MessageID, "Text added.")
					contentAdded = true
				}
			}
			if contentAdded {
				kb, _ := b.buildConfirmTargetsMarkup(ctx, s.PostID)
				m := tgbotapi.NewMessage(message.Chat.ID, "Select platforms and press Confirm when ready.")
				m.ReplyMarkup = kb
				_, _ = b.api.Send(m)
			} else {
				_, _ = b.SendReply(message.Chat.ID, message.MessageID, "Please send photos/videos and text in captions, then press Confirm.")
			}
			return
		}
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
	case "post":
		b.handlePostCommand(message)
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
	// If this is part of the post setup flow (ps:*), route directly and return.
	if action == "ps" {
		b.handlePostSetupCallback(query)
		return
	}
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
		// Post setup actions: ps:toggle, ps:confirm, ps:cancel
		if strings.HasPrefix(action, "ps") {
			b.handlePostSetupCallback(query)
		} else {
			_, _ = b.api.Request(tgbotapi.NewCallback(query.ID, "Unknown action"))
		}
	}
}

// handlePostCommand starts the guided post creation flow
func (b *Bot) handlePostCommand(message *tgbotapi.Message) {
	ctx, cancel := b.dbCtx()
	defer cancel()
	// Create draft
	postID, err := b.repo.CreatePost(ctx, &storage.Post{
		TelegramUserID: message.From.ID,
		ChatID:         message.Chat.ID,
		MessageID:      message.MessageID,
		Type:           "text",
		TextContent:    "",
	})
	if err != nil {
		slog.Error("create post (cmd) error", "err", err)
		_, _ = b.SendReply(message.Chat.ID, message.MessageID, "Failed to start post creation. Please try again.")
		return
	}
	// Compose mode prompt: ask for media/text first; checklist after content arrives
	b.setSession(message.Chat.ID, &PostSession{PostID: postID, Step: "compose"})
	row := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Cancel", fmt.Sprintf("ps:cancel:%d", postID)),
	)
	m := tgbotapi.NewMessage(message.Chat.ID, "Please send pictures (up to 10) and text for the post (in a caption).")
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(row)
	_, _ = b.api.Send(m)
}

func (b *Bot) handlePostSetupCallback(q *tgbotapi.CallbackQuery) {
	parts := strings.Split(q.Data, ":")
	if len(parts) < 2 {
		_, _ = b.api.Request(tgbotapi.NewCallback(q.ID, "Invalid"))
		return
	}
	action := parts[1]
	// Expect formats: ps:toggle:<postID>:<platform>, ps:confirm:<postID>, ps:cancel:<postID>
	switch action {
	case "toggle":
		if len(parts) != 4 {
			_, _ = b.api.Request(tgbotapi.NewCallback(q.ID, "Invalid toggle"))
			return
		}
		postID, _ := strconv.ParseInt(parts[2], 10, 64)
		platform := parts[3]
		ctx, cancel := b.dbCtx()
		defer cancel()
		if _, err := b.repo.ToggleTarget(ctx, postID, platform); err != nil {
			slog.Error("toggle (setup) error", "err", err)
		}
		kb, _ := b.buildConfirmTargetsMarkup(ctx, postID)
		edit := tgbotapi.NewEditMessageReplyMarkup(q.Message.Chat.ID, q.Message.MessageID, kb)
		_, _ = b.api.Request(edit)
		_, _ = b.api.Request(tgbotapi.NewCallback(q.ID, "Updated"))
	case "confirm":
		if len(parts) != 3 {
			_, _ = b.api.Request(tgbotapi.NewCallback(q.ID, "Invalid"))
			return
		}
		postID, _ := strconv.ParseInt(parts[2], 10, 64)
		ctx, cancel := b.dbCtx()
		defer cancel()
		_ = b.repo.SetPostStatus(ctx, postID, "queued")
		if err := b.publishSelected(ctx, postID); err != nil {
			slog.Error("publish (confirm) error", "err", err, "post_id", postID)
			_, _ = b.api.Send(tgbotapi.NewMessage(q.Message.Chat.ID, fmt.Sprintf("Publish failed: %v", err)))
		} else {
			_, _ = b.api.Send(tgbotapi.NewMessage(q.Message.Chat.ID, "Published to selected platforms."))
		}
		b.clearSession(q.Message.Chat.ID)
		_, _ = b.api.Request(tgbotapi.NewCallback(q.ID, "Done"))
	case "cancel":
		if len(parts) != 3 {
			_, _ = b.api.Request(tgbotapi.NewCallback(q.ID, "Invalid"))
			return
		}
		postID, _ := strconv.ParseInt(parts[2], 10, 64)
		ctx, cancel := b.dbCtx()
		defer cancel()
		_ = b.repo.SetPostStatus(ctx, postID, "canceled")
		b.clearSession(q.Message.Chat.ID)
		_, _ = b.api.Send(tgbotapi.NewMessage(q.Message.Chat.ID, "Post creation canceled."))
		_, _ = b.api.Request(tgbotapi.NewCallback(q.ID, "Canceled"))
	default:
		_, _ = b.api.Request(tgbotapi.NewCallback(q.ID, "Unknown"))
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
/post - Create a new post: send photos/videos (up to 10) and/or text → select platforms → confirm
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

// buildSetupTargetsMarkup is like buildTargetsMarkup but uses ps:toggle callbacks and appends Next/Cancel row.
func (b *Bot) buildSetupTargetsMarkup(ctx context.Context, postID int64) (tgbotapi.InlineKeyboardMarkup, error) {
	selected, err := b.repo.ListTargets(ctx, postID)
	if err != nil {
		return tgbotapi.InlineKeyboardMarkup{}, err
	}
	btn := func(name, key string) tgbotapi.InlineKeyboardButton {
		label := name
		if selected[key] {
			label = "✅ " + name
		}
		return tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("ps:toggle:%d:%s", postID, key))
	}
	row1 := tgbotapi.NewInlineKeyboardRow(
		btn("Twitter", "twitter"), btn("Pinterest", "pinterest"), btn("Facebook", "facebook"),
	)
	row2 := tgbotapi.NewInlineKeyboardRow(
		btn("Instagram", "instagram"), btn("TikTok", "tiktok"),
	)
	next := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Next ▶️ Media", fmt.Sprintf("ps:media:%d", postID)),
		tgbotapi.NewInlineKeyboardButtonData("Cancel", fmt.Sprintf("ps:cancel:%d", postID)),
	)
	return tgbotapi.NewInlineKeyboardMarkup(row1, row2, next), nil
}

// buildConfirmTargetsMarkup shows toggles and Confirm/Cancel.
func (b *Bot) buildConfirmTargetsMarkup(ctx context.Context, postID int64) (tgbotapi.InlineKeyboardMarkup, error) {
	selected, err := b.repo.ListTargets(ctx, postID)
	if err != nil {
		return tgbotapi.InlineKeyboardMarkup{}, err
	}
	btn := func(name, key string) tgbotapi.InlineKeyboardButton {
		label := name
		if selected[key] {
			label = "✅ " + name
		}
		return tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("ps:toggle:%d:%s", postID, key))
	}
	row1 := tgbotapi.NewInlineKeyboardRow(
		btn("Twitter", "twitter"), btn("Pinterest", "pinterest"), btn("Facebook", "facebook"),
	)
	row2 := tgbotapi.NewInlineKeyboardRow(
		btn("Instagram", "instagram"), btn("TikTok", "tiktok"),
	)
	actions := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Confirm ✅", fmt.Sprintf("ps:confirm:%d", postID)),
		tgbotapi.NewInlineKeyboardButtonData("Cancel", fmt.Sprintf("ps:cancel:%d", postID)),
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
	if selections["facebook"] {
		if err := b.publishToFacebook(ctx, post); err != nil {
			return err
		}
	}
	if selections["instagram"] {
		if err := b.publishToInstagram(ctx, post); err != nil {
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
	// Prefer multiple from post_media; fallback to single PhotoFileID
	if items, err := b.repo.ListMedia(ctx, p.ID); err == nil && len(items) > 0 {
		max := len(items)
		if max > 4 {
			max = 4
		}
		for i := 0; i < max; i++ {
			if strings.ToLower(items[i].Type) != "photo" {
				continue
			}
			data, ctype, err := b.downloadTelegramFile(ctx, items[i].FileID)
			if err != nil {
				continue
			}
			if ctype == "application/octet-stream" || ctype == "" {
				ctype = "image/jpeg"
			}
			media = append(media, data)
			mediaTypes = append(mediaTypes, ctype)
		}
	} else if p.PhotoFileID != nil {
		data, ctype, err := b.downloadTelegramFile(ctx, *p.PhotoFileID)
		if err == nil {
			if ctype == "application/octet-stream" || ctype == "" {
				ctype = "image/jpeg"
			}
			media = append(media, data)
			mediaTypes = append(mediaTypes, ctype)
		}
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
	// Use first media if available, else single photo
	var fileID string
	if items, err := b.repo.ListMedia(ctx, p.ID); err == nil && len(items) > 0 {
		for _, it := range items {
			if strings.ToLower(it.Type) == "photo" {
				fileID = it.FileID
				break
			}
		}
	} else {
		if p.PhotoFileID != nil {
			fileID = *p.PhotoFileID
		}
	}
	if fileID == "" {
		msg := "Pinterest requires an image"
		_ = b.repo.SetTargetStatus(ctx, p.ID, "pinterest", "failed", nil, &msg)
		_ = b.repo.AddLog(ctx, p.ID, ptr("pinterest"), "error", msg)
		return fmt.Errorf(msg)
	}
	// Download image from Telegram
	data, ctype, err := b.downloadTelegramFile(ctx, fileID)
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

func (b *Bot) publishToFacebook(ctx context.Context, p *storage.Post) error {
	if b.config.FacebookAccessToken == "" || b.config.FacebookPageID == "" {
		msg := "Facebook access token or page ID missing"
		_ = b.repo.SetTargetStatus(ctx, p.ID, "facebook", "failed", nil, &msg)
		_ = b.repo.AddLog(ctx, p.ID, ptr("facebook"), "error", msg)
		return fmt.Errorf(msg)
	}
	cli, err := facebook.New(facebook.Credentials{AccessToken: b.config.FacebookAccessToken})
	if err != nil {
		_ = b.repo.SetTargetStatus(ctx, p.ID, "facebook", "failed", nil, strptr(err.Error()))
		_ = b.repo.AddLog(ctx, p.ID, ptr("facebook"), "error", err.Error())
		return err
	}
	var img []byte
	var ctype string
	if items, err := b.repo.ListMedia(ctx, p.ID); err == nil && len(items) > 0 {
		var fid string
		for _, it := range items {
			if strings.ToLower(it.Type) == "photo" {
				fid = it.FileID
				break
			}
		}
		if fid != "" {
			img, ctype, err = b.downloadTelegramFile(ctx, fid)
			if err != nil {
				img = nil
			}
		}
	} else if p.PhotoFileID != nil {
		img, ctype, err = b.downloadTelegramFile(ctx, *p.PhotoFileID)
		if err != nil {
			_ = b.repo.SetTargetStatus(ctx, p.ID, "facebook", "failed", nil, strptr(err.Error()))
			_ = b.repo.AddLog(ctx, p.ID, ptr("facebook"), "error", "telegram download: "+err.Error())
			return err
		}
	}
	id, err := cli.CreatePost(ctx, b.config.FacebookPageID, p.TextContent, img, ctype)
	if err != nil {
		_ = b.repo.SetTargetStatus(ctx, p.ID, "facebook", "failed", nil, strptr(err.Error()))
		_ = b.repo.AddLog(ctx, p.ID, ptr("facebook"), "error", err.Error())
		return err
	}
	_ = b.repo.SetTargetStatus(ctx, p.ID, "facebook", "published", &id, nil)
	_ = b.repo.AddLog(ctx, p.ID, ptr("facebook"), "published", "id="+id)
	return nil
}

func (b *Bot) publishToInstagram(ctx context.Context, p *storage.Post) error {
	if b.config.InstagramAccessToken == "" || b.config.InstagramUserID == "" {
		msg := "Instagram access token or user ID missing"
		_ = b.repo.SetTargetStatus(ctx, p.ID, "instagram", "failed", nil, &msg)
		_ = b.repo.AddLog(ctx, p.ID, ptr("instagram"), "error", msg)
		return fmt.Errorf(msg)
	}
	// Prefer first media if present
	var fileID string
	if items, err := b.repo.ListMedia(ctx, p.ID); err == nil && len(items) > 0 {
		for _, it := range items {
			if strings.ToLower(it.Type) == "photo" {
				fileID = it.FileID
				break
			}
		}
	} else {
		if p.PhotoFileID != nil {
			fileID = *p.PhotoFileID
		}
	}
	if fileID == "" {
		msg := "Instagram requires an image"
		_ = b.repo.SetTargetStatus(ctx, p.ID, "instagram", "failed", nil, &msg)
		_ = b.repo.AddLog(ctx, p.ID, ptr("instagram"), "error", msg)
		return fmt.Errorf(msg)
	}
	imgURL, err := b.getTelegramFileURL(fileID)
	if err != nil {
		_ = b.repo.SetTargetStatus(ctx, p.ID, "instagram", "failed", nil, strptr(err.Error()))
		_ = b.repo.AddLog(ctx, p.ID, ptr("instagram"), "error", "telegram get url: "+err.Error())
		return err
	}
	cli, err := instagram.New(instagram.Credentials{AccessToken: b.config.InstagramAccessToken})
	if err != nil {
		_ = b.repo.SetTargetStatus(ctx, p.ID, "instagram", "failed", nil, strptr(err.Error()))
		_ = b.repo.AddLog(ctx, p.ID, ptr("instagram"), "error", err.Error())
		return err
	}
	id, err := cli.CreatePhotoPost(ctx, b.config.InstagramUserID, p.TextContent, imgURL)
	if err != nil {
		_ = b.repo.SetTargetStatus(ctx, p.ID, "instagram", "failed", nil, strptr(err.Error()))
		_ = b.repo.AddLog(ctx, p.ID, ptr("instagram"), "error", err.Error())
		return err
	}
	_ = b.repo.SetTargetStatus(ctx, p.ID, "instagram", "published", &id, nil)
	_ = b.repo.AddLog(ctx, p.ID, ptr("instagram"), "published", "id="+id)
	return nil
}

func ptr[T any](v T) *T       { return &v }
func strptr(s string) *string { return &s }
