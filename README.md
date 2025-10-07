# Telegram Bot in Go

A simple and clean Telegram bot written in Go using the official Telegram Bot API.

## Features

- Handles commands, messages, callback queries, and inline queries
- Supports both long polling and webhook modes
- Configurable through environment variables
- Docker support for easy deployment
- Middleware for logging and authorization
- Clean architecture with separation of concerns

## Setup

### Prerequisites

- Go 1.24 or later
- A Telegram Bot Token (get it from [BotFather](https://t.me/BotFather))

### Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/yourusername/telegrambot.git
   cd telegrambot
   ```

2. Copy the example environment file and add your bot token and DB variables:
   ```bash
   cp .env.example .env
   # Edit .env and add your Telegram bot token and DATABASE_URL
   ```

3. Build and run the bot:
   ```bash
   go build -o telegrambot ./cmd/bot
   ./telegrambot
   ```

### Using Docker

1. Start Postgres and the bot with Docker Compose (defined under `docker/`):
   ```bash
   cd docker
   docker compose up -d
   ```

The compose file includes a `postgres:16-alpine` service and sets the bot's `DATABASE_URL` to the Postgres container by default.

If you run the bot directly on your host (without Docker), ensure Postgres is running locally and set `DATABASE_URL`, for example:
```
DATABASE_URL=postgres://trinitybot:trinitybot@localhost:5432/trinitybot?sslmode=disable
```

## Configuration

The bot can be configured using environment variables:

- `TELEGRAM_TOKEN` (required): Your Telegram Bot API token
- `DEBUG_MODE` (optional): Set to "true" for verbose logging
- `WEBHOOK_URL` (optional): URL for webhook mode (if not set, uses long polling)
- `PORT` (optional): Port for webhook server (default: 8443)
- `ALLOWED_USERS` (optional): Comma-separated list of allowed user IDs
- `DATABASE_URL` (recommended): Postgres connection string. If empty, the app falls back to `POSTGRES_*` variables.
- `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` (fallback if `DATABASE_URL` not set)

## Project Structure

```
project-root/
├── cmd/
│   └── bot/
│       └── main.go               # Application entry point
├── internal/
│   ├── bot/
│   │   ├── bot.go                # Bot struct and core functionality
│   │   ├── handlers.go           # Telegram message/command handlers
│   │   └── middleware.go         # Any middleware for handling messages
│   ├── config/
│   │   └── config.go             # Configuration loading and management
│   ├── db/
│   │   ├── db.go                 # Postgres connection + embedded migration runner
│   │   └── migrations/
│   │       └── 0001_init.sql     # Initial schema for posts/targets/logs
│   └── service/
│       └── service.go            # Business logic services
│   └── storage/
│       └── posts.go              # Post repository (CRUD + targets)
├── pkg/
│   └── utils/
│       └── utils.go              # Shared utility functions
├── .env.example                  # Example environment variables file
├── .gitignore                    # Git ignore file
├── docker-compose.yml            # Docker configuration
├── Dockerfile                    # Dockerfile for containerization
├── go.mod                        # Go modules file
├── go.sum                        # Go modules checksum file
└── README.md                     # Project documentation
```

## Extending the Bot

### Migrations

- SQL migrations live under `internal/db/migrations`. They are embedded into the binary and auto-applied on startup.
- Applied migrations are tracked in the `schema_migrations` table.

### Telegram Flow: Drafts and Targets

- Send a text message or a photo with caption to create a draft post.
- The bot replies with an inline keyboard to select target platforms (Twitter, Pinterest, Facebook, Instagram, TikTok).
- Press "Publish" to queue the post (integrations will be wired next).

### Adding a New Command

1. Add a new command handler in `internal/bot/handlers.go`:
   ```go
   func (b *Bot) handleNewCommand(message *tgbotapi.Message) {
       // Your command logic here
       b.SendMessage(message.Chat.ID, "New command response")
   }
   ```

2. Register the command in the command handler:
   ```go
   switch strings.ToLower(command) {
   // ... existing commands
   case "newcommand":
       b.handleNewCommand(message)
   }
   ```

### Adding a Service Layer

1. Define a new service interface in `internal/service/service.go`:
   ```go
   type NewService interface {
       DoSomething(param string) (string, error)
   }
   ```

2. Implement the service:
   ```go
   type newServiceImpl struct {}
   
   func NewNewService() NewService {
       return &newServiceImpl{}
   }
   
   func (s *newServiceImpl) DoSomething(param string) (string, error) {
       // Implementation here
       return result, nil
   }
   ```

3. Use the service in your bot handlers:
   ```go
   newService := service.NewNewService()
   result, err := newService.DoSomething("param")
   ```

## License

This project is licensed under the MIT License - see the LICENSE file for details.
