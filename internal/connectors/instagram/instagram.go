package instagram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const graphHost = "https://graph.facebook.com/v19.0"

type Client struct {
	httpClient  *http.Client
	accessToken string
}

type Credentials struct {
	AccessToken string
}

func New(creds Credentials) (*Client, error) {
	if creds.AccessToken == "" {
		return nil, errors.New("instagram access token missing")
	}
	return &Client{httpClient: &http.Client{Timeout: 25 * time.Second}, accessToken: creds.AccessToken}, nil
}

// CreatePhotoPost creates and publishes a photo post using a publicly accessible image URL.
// Returns the published media id.
func (c *Client) CreatePhotoPost(ctx context.Context, igUserID, caption, imageURL string) (string, error) {
	if c == nil || c.httpClient == nil {
		return "", errors.New("instagram client not initialized")
	}
	if igUserID == "" {
		return "", errors.New("instagram user id missing")
	}
	if imageURL == "" {
		return "", errors.New("image_url required for instagram")
	}
	// Step 1: create container
	createVals := url.Values{}
	createVals.Set("image_url", imageURL)
	if caption != "" {
		createVals.Set("caption", caption)
	}
	createVals.Set("access_token", c.accessToken)
	createURL := fmt.Sprintf("%s/%s/media", graphHost, igUserID)
	req1, err := http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewBufferString(createVals.Encode()))
	if err != nil {
		return "", err
	}
	req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp1, err := c.httpClient.Do(req1)
	if err != nil {
		return "", err
	}
	defer resp1.Body.Close()
	if resp1.StatusCode < 200 || resp1.StatusCode >= 300 {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp1.Body).Decode(&apiErr)
		return "", fmt.Errorf("instagram media create status %d: %s", resp1.StatusCode, apiErr.Error.Message)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp1.Body).Decode(&created); err != nil {
		return "", err
	}
	if created.ID == "" {
		return "", errors.New("instagram: missing creation id")
	}

	// Step 2: publish
	pubVals := url.Values{}
	pubVals.Set("creation_id", created.ID)
	pubVals.Set("access_token", c.accessToken)
	pubURL := fmt.Sprintf("%s/%s/media_publish", graphHost, igUserID)
	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, pubURL, bytes.NewBufferString(pubVals.Encode()))
	if err != nil {
		return "", err
	}
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp2, err := c.httpClient.Do(req2)
	if err != nil {
		return "", err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode < 200 || resp2.StatusCode >= 300 {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp2.Body).Decode(&apiErr)
		return "", fmt.Errorf("instagram media publish status %d: %s", resp2.StatusCode, apiErr.Error.Message)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("instagram: missing media id in response")
	}
	return out.ID, nil
}
