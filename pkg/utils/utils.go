package utils

import (
	"fmt"
	"strings"
	"time"
)

// FormatDuration formats a duration in a human-readable format
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%d hours", int(d.Hours()))
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}

// TruncateText truncates text to a specified length, adding an ellipsis if needed
func TruncateText(text string, maxLength int) string {
	if len(text) <= maxLength {
		return text
	}
	return text[:maxLength-3] + "..."
}

// SplitMessageByLimit splits a message into chunks that fit within Telegram's message length limit
func SplitMessageByLimit(message string, limit int) []string {
	if len(message) <= limit {
		return []string{message}
	}

	var chunks []string
	var current string
	lines := strings.Split(message, "\n")

	for _, line := range lines {
		if len(current)+len(line)+1 > limit {
			if current != "" {
				chunks = append(chunks, current)
				current = line
			} else {
				// If a single line is longer than the limit, we need to split it
				for len(line) > 0 {
					if len(line) <= limit {
						chunks = append(chunks, line)
						line = ""
					} else {
						chunks = append(chunks, line[:limit])
						line = line[limit:]
					}
				}
			}
		} else {
			if current != "" {
				current += "\n" + line
			} else {
				current = line
			}
		}
	}

	if current != "" {
		chunks = append(chunks, current)
	}

	return chunks
}
