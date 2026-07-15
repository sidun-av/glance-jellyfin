package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	HTTPClient *http.Client
	BaseURL    string
	Token      string
	UserID     string
}

func New(baseURL, token, userID string) *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Token:      token,
		UserID:     userID,
	}
}

type Item struct {
	ID       string
	Name     string
	HasImage bool
}

func (c *Client) FetchLatest(ctx context.Context, limit int) ([]Item, error) {
	u := fmt.Sprintf("%s/Users/%s/Items/Latest", c.BaseURL, c.UserID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	q := req.URL.Query()
	q.Set("IncludeItemTypes", "Movie,Series")
	q.Set("Limit", strconv.Itoa(limit))
	q.Set("GroupItems", "false")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("X-Emby-Token", c.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request latest items: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("latest items returned status %d", resp.StatusCode)
	}

	type rawItem struct {
		ID        string `json:"Id"`
		Name      string `json:"Name"`
		ImageTags struct {
			Primary string `json:"Primary"`
		} `json:"ImageTags"`
	}
	var rawItems []rawItem
	if err := json.NewDecoder(resp.Body).Decode(&rawItems); err != nil {
		return nil, fmt.Errorf("parse latest items response: %w", err)
	}

	items := make([]Item, len(rawItems))
	for i, r := range rawItems {
		items[i] = Item{ID: r.ID, Name: r.Name, HasImage: r.ImageTags.Primary != ""}
	}
	return items, nil
}

type ImageResult struct {
	Body        io.ReadCloser
	ContentType string
	StatusCode  int
}

// FetchImage streams a poster image from Jellyfin. The caller owns Body and
// must close it. A non-200 StatusCode is not treated as an error — the
// caller (main.go's imageHandler) decides how to respond (e.g. a 404 for a
// missing poster).
func (c *Client) FetchImage(ctx context.Context, itemID string) (*ImageResult, error) {
	u := fmt.Sprintf("%s/Items/%s/Images/Primary", c.BaseURL, itemID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Emby-Token", c.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request image: %w", err)
	}

	return &ImageResult{
		Body:        resp.Body,
		ContentType: resp.Header.Get("Content-Type"),
		StatusCode:  resp.StatusCode,
	}, nil
}

// FetchServerID retrieves Jellyfin's own server ID via its unauthenticated
// public info endpoint, used to build a direct playback deep link
// (web/#/video?id=...&serverId=...). The caller (main.go's
// app.fetchServerIDCached) fetches this once and caches it for the process
// lifetime, since a running server's ID cannot change without a restart.
func (c *Client) FetchServerID(ctx context.Context) (string, error) {
	u := fmt.Sprintf("%s/System/Info/Public", c.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request server info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server info returned status %d", resp.StatusCode)
	}

	var raw struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return "", fmt.Errorf("parse server info response: %w", err)
	}
	return raw.ID, nil
}
