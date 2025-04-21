package bot

import (
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
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

	// Handle regular messages
	log.Printf("Received message from %s: %s", message.From.UserName, message.Text)

	// Echo the message back (replace with your logic)
	_, err := b.SendReply(message.Chat.ID, message.MessageID, "You said: "+message.Text)
	if err != nil {
		log.Printf("Error sending message: %v", err)
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

	// Acknowledge the callback query
	callbackResponse := tgbotapi.NewCallback(query.ID, "")
	_, err := b.api.Request(callbackResponse)
	if err != nil {
		log.Printf("Error answering callback query: %v", err)
	}

	// Process the callback data (replace with your logic)
	msg := tgbotapi.NewMessage(query.Message.Chat.ID, "You clicked: "+query.Data)
	_, err = b.api.Send(msg)
	if err != nil {
		log.Printf("Error sending message: %v", err)
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
`
	_, err := b.SendMessage(message.Chat.ID, helpText)
	if err != nil {
		log.Printf("Error sending message: %v", err)
	}
}
