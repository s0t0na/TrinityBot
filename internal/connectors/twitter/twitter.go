package twitter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dghubble/oauth1"
	tw "github.com/g8rswimmer/go-twitter/v2"
)

type Client struct {
	api *tw.Client
}

type Credentials struct {
	ConsumerKey    string
	ConsumerSecret string
	AccessToken    string
	AccessSecret   string
}

// noopAuthorizer lets the underlying oauth1 http.Client sign requests.
type noopAuthorizer struct{}

func (n noopAuthorizer) Add(req *http.Request) {}

func New(creds Credentials) (*Client, error) {
	if creds.ConsumerKey == "" || creds.ConsumerSecret == "" || creds.AccessToken == "" || creds.AccessSecret == "" {
		return nil, errors.New("twitter credentials incomplete")
	}
	conf := oauth1.NewConfig(creds.ConsumerKey, creds.ConsumerSecret)
	token := oauth1.NewToken(creds.AccessToken, creds.AccessSecret)
	httpClient := conf.Client(context.Background(), token)
	httpClient.Timeout = 20 * time.Second

	api := &tw.Client{
		Authorizer: noopAuthorizer{},
		Client:     httpClient,
		Host:       "https://api.twitter.com",
	}
	return &Client{api: api}, nil
}

// Publish posts a text tweet using Twitter API v2.
func (c *Client) Publish(ctx context.Context, text string, mediaContents [][]byte, mediaTypes []string) (string, error) {
	if c == nil || c.api == nil {
		return "", errors.New("twitter client nil")
	}
	// Upload up to 4 images via v1.1 media/upload and tweet with media_ids
	var mediaIDs []string
	if len(mediaContents) > 0 {
		if len(mediaContents) != len(mediaTypes) {
			return "", fmt.Errorf("len(mediaContents) != len(mediaTypes)")
		}
		count := len(mediaContents)
		if count > 4 {
			count = 4
		}
		for i := 0; i < count; i++ {
			ctype := strings.ToLower(mediaTypes[i])
			if !strings.HasPrefix(ctype, "image/") {
				// skip non-image for now
				continue
			}
			id, err := c.uploadSimpleMedia(ctx, mediaContents[i])
			if err != nil {
				return "", fmt.Errorf("media upload failed: %w", err)
			}
			mediaIDs = append(mediaIDs, id)
		}
	}

	req := tw.CreateTweetRequest{Text: text}
	if len(mediaIDs) > 0 {
		req.Media = &tw.CreateTweetMedia{IDs: mediaIDs}
	}
	resp, err := c.api.CreateTweet(ctx, req)
	if err != nil {
		return "", fmt.Errorf("create tweet: %w", err)
	}
	if resp == nil || resp.Tweet == nil {
		return "", errors.New("empty response from twitter")
	}
	return resp.Tweet.ID, nil
}

// uploadSimpleMedia uploads an image using v1.1 simple upload and returns media_id_string.
func (c *Client) uploadSimpleMedia(ctx context.Context, b []byte) (string, error) {
	if c == nil || c.api == nil || c.api.Client == nil {
		return "", errors.New("nil twitter http client")
	}
	// Base64 encode content per simple upload
	enc := base64.StdEncoding.EncodeToString(b)
	form := url.Values{}
	form.Set("media", enc)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://upload.twitter.com/1.1/media/upload.json", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.api.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload status %d: %s", resp.StatusCode, string(body))
	}
	// Parse JSON response
	var out struct {
		MediaIDString string `json:"media_id_string"`
		MediaID       int64  `json:"media_id"`
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&out); err != nil {
		return "", err
	}
	if out.MediaIDString != "" {
		return out.MediaIDString, nil
	}
	if out.MediaID != 0 {
		return fmt.Sprintf("%d", out.MediaID), nil
	}
	return "", errors.New("missing media id in response")
}
