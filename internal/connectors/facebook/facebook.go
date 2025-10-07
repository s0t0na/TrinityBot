package facebook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
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
		return nil, errors.New("facebook access token missing")
	}
	return &Client{httpClient: &http.Client{Timeout: 25 * time.Second}, accessToken: creds.AccessToken}, nil
}

// CreatePost posts to a Facebook Page. If image is provided, uploads a photo with optional caption; otherwise posts a text status.
// Returns the created object id (photo id or post id).
func (c *Client) CreatePost(ctx context.Context, pageID string, message string, image []byte, contentType string) (string, error) {
	if c == nil || c.httpClient == nil {
		return "", errors.New("facebook client not initialized")
	}
	if pageID == "" {
		return "", errors.New("facebook page id missing")
	}
	if len(image) > 0 {
		return c.uploadPhoto(ctx, pageID, message, image, contentType)
	}
	return c.postFeed(ctx, pageID, message)
}

func (c *Client) postFeed(ctx context.Context, pageID, message string) (string, error) {
	form := url.Values{}
	if message != "" {
		form.Set("message", message)
	}
	form.Set("access_token", c.accessToken)
	endpoint := fmt.Sprintf("%s/%s/feed", graphHost, pageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("facebook feed status %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("facebook: missing id in response")
	}
	return out.ID, nil
}

func (c *Client) uploadPhoto(ctx context.Context, pageID, caption string, image []byte, contentType string) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	// caption
	if caption != "" {
		_ = mw.WriteField("caption", caption)
	}
	// published true by default; ensure it
	_ = mw.WriteField("published", "true")
	// access token
	_ = mw.WriteField("access_token", c.accessToken)

	// file part
	fh := make(textproto.MIMEHeader)
	fh.Set("Content-Disposition", "form-data; name=\"source\"; filename=\"image.jpg\"")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	fh.Set("Content-Type", contentType)
	part, err := mw.CreatePart(fh)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(image); err != nil {
		return "", err
	}
	_ = mw.Close()

	endpoint := fmt.Sprintf("%s/%s/photos", graphHost, pageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("facebook photo status %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("facebook: missing id in response")
	}
	return out.ID, nil
}
