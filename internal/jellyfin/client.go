package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
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
