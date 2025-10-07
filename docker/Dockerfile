FROM golang:1.24-alpine AS builder

# Install git and ca-certificates (needed for private repos)
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o /trinitybot ./cmd/bot

# Create a minimal image
FROM alpine:latest

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

# Copy the binary from the builder stage
COPY --from=builder /trinitybot /trinitybot

# Set the binary as the entrypoint
ENTRYPOINT ["/trinitybot"]
