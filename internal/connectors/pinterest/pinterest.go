package pinterest

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	httpClient  *http.Client
	accessToken string
}

type Credentials struct {
	AccessToken string
}

func New(creds Credentials) (*Client, error) {
	if creds.AccessToken == "" {
		return nil, errors.New("pinterest access token missing")
	}
	return &Client{
		httpClient:  &http.Client{Timeout: 25 * time.Second},
		accessToken: creds.AccessToken,
	}, nil
}

// CreatePin creates a pin on the given board using base64 image.
// Returns the created pin ID.
func (c *Client) CreatePin(ctx context.Context, boardID, title, description, link string, image []byte, contentType string) (string, error) {
	if c == nil || c.httpClient == nil {
		return "", errors.New("pinterest client not initialized")
	}
	if boardID == "" {
		return "", errors.New("pinterest board id missing")
	}
	if len(image) == 0 {
		return "", errors.New("pinterest requires an image for a pin")
	}
	// Build payload
	payload := struct {
		Title       string `json:"title,omitempty"`
		Description string `json:"description,omitempty"`
		Link        string `json:"link,omitempty"`
		BoardID     string `json:"board_id"`
		MediaSource struct {
			SourceType  string `json:"source_type"`
			ContentType string `json:"content_type"`
			Data        string `json:"data"`
		} `json:"media_source"`
	}{
		Title:       title,
		Description: description,
		Link:        link,
		BoardID:     boardID,
	}
	if contentType == "" {
		contentType = "image/jpeg"
	}
	payload.MediaSource.SourceType = "image_base64"
	payload.MediaSource.ContentType = contentType
	payload.MediaSource.Data = base64.StdEncoding.EncodeToString(image)

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.pinterest.com/v5/pins", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		if apiErr.Message == "" {
			apiErr.Message = resp.Status
		}
		return "", fmt.Errorf("pinterest create pin status %d: %s", resp.StatusCode, apiErr.Message)
	}

	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("pinterest: missing pin id in response")
	}
	return out.ID, nil
}

// no extra helpers
