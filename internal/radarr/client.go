package radarr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	HTTPClient *http.Client
	BaseURL    string
	APIKey     string
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
	}
}

type QueueItem struct {
	MovieID  int
	Title    string
	Size     int64
	SizeLeft int64
}

func (c *Client) FetchQueue(ctx context.Context) ([]QueueItem, error) {
	req, err := c.newRequest(ctx, "/api/v3/queue", map[string]string{
		"includeMovie": "true",
		"pageSize":     "200",
	})
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request queue: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("queue returned status %d", resp.StatusCode)
	}

	var raw struct {
		Records []struct {
			MovieID  int   `json:"movieId"`
			Size     int64 `json:"size"`
			SizeLeft int64 `json:"sizeleft"`
			Movie    struct {
				Title string `json:"title"`
			} `json:"movie"`
		} `json:"records"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse queue response: %w", err)
	}

	items := make([]QueueItem, len(raw.Records))
	for i, r := range raw.Records {
		items[i] = QueueItem{MovieID: r.MovieID, Title: r.Movie.Title, Size: r.Size, SizeLeft: r.SizeLeft}
	}
	return items, nil
}

type MissingItem struct {
	MovieID int
	Title   string
}

func (c *Client) FetchMissing(ctx context.Context) ([]MissingItem, error) {
	req, err := c.newRequest(ctx, "/api/v3/wanted/missing", map[string]string{
		"monitored": "true",
		"pageSize":  "200",
	})
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request wanted/missing: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wanted/missing returned status %d", resp.StatusCode)
	}

	var raw struct {
		Records []struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
		} `json:"records"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse wanted/missing response: %w", err)
	}

	items := make([]MissingItem, len(raw.Records))
	for i, r := range raw.Records {
		items[i] = MissingItem{MovieID: r.ID, Title: r.Title}
	}
	return items, nil
}

type ImageResult struct {
	Body        io.ReadCloser
	ContentType string
	StatusCode  int
}

// FetchPoster streams a movie's poster image from Radarr. The caller owns
// Body and must close it. A non-200 StatusCode is not treated as an error —
// mirrors jellyfin.Client.FetchImage's contract.
func (c *Client) FetchPoster(ctx context.Context, movieID string) (*ImageResult, error) {
	u := fmt.Sprintf("%s/api/v3/MediaCover/%s/poster.jpg", c.BaseURL, movieID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Api-Key", c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request poster: %w", err)
	}

	return &ImageResult{Body: resp.Body, ContentType: resp.Header.Get("Content-Type"), StatusCode: resp.StatusCode}, nil
}

func (c *Client) newRequest(ctx context.Context, path string, query map[string]string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	q := req.URL.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("X-Api-Key", c.APIKey)
	req.Header.Set("Accept", "application/json")
	return req, nil
}
