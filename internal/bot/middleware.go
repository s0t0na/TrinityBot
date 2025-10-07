package bot

import (
	"log/slog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// applyMiddleware applies middleware to an update
// Returns true if processing should continue, false if it should stop
func (b *Bot) applyMiddleware(update tgbotapi.Update) bool {
	// Log incoming updates
	if b.config.DebugMode {
		logUpdate(update)
	}

	// Check if user is authorized (if allowed users are specified)
	if !b.isAuthorized(update) {
		return false
	}

	return true
}

// logUpdate logs information about an update
func logUpdate(update tgbotapi.Update) {
	var userID int64
	var userName string
	var updateType string
	var content string

	if update.Message != nil {
		userID = update.Message.From.ID
		userName = update.Message.From.UserName
		updateType = "Message"
		content = update.Message.Text
	} else if update.CallbackQuery != nil {
		userID = update.CallbackQuery.From.ID
		userName = update.CallbackQuery.From.UserName
		updateType = "CallbackQuery"
		content = update.CallbackQuery.Data
	} else if update.InlineQuery != nil {
		userID = update.InlineQuery.From.ID
		userName = update.InlineQuery.From.UserName
		updateType = "InlineQuery"
		content = update.InlineQuery.Query
	} else {
		updateType = "Other"
	}

	slog.Debug("Update received",
		"type", updateType,
		"user", userName,
		"user_id", userID,
		"content", content,
	)
}

// isAuthorized checks if a user is authorized to use the bot
func (b *Bot) isAuthorized(update tgbotapi.Update) bool {
	// If no allowed users are specified, everyone is allowed
	if len(b.config.AllowedUsers) == 0 {
		return true
	}

	var userID int64
	if update.Message != nil {
		userID = update.Message.From.ID
	} else if update.CallbackQuery != nil {
		userID = update.CallbackQuery.From.ID
	} else if update.InlineQuery != nil {
		userID = update.InlineQuery.From.ID
	} else {
		// Can't determine user for this update type
		return false
	}

	// Check if the user is in the allowed list
	for _, id := range b.config.AllowedUsers {
		if id == userID {
			return true
		}
	}

	// User not in allowed list
	slog.Warn("Unauthorized access attempt", "user_id", userID)
	return false
}
