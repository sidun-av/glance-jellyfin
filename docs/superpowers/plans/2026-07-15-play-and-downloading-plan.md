# Play Button + Downloading Section Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a direct "Play" link to Library cards and a new "Downloading" section (Radarr/Sonarr queue + wanted, live-updating progress) to the existing `glance-jellyfin` widget, per `docs/superpowers/specs/2026-07-15-play-and-downloading-design.md`.

**Architecture:** Two new thin REST clients (`internal/radarr`, `internal/sonarr`) mirroring `internal/jellyfin/client.go`'s shape. A background poller (`poller.go`) refreshes a cached snapshot every 10s; `/widget` and a new `/live.json` both read that cached snapshot (never call Radarr/Sonarr directly). The rendered HTML uses the same `onerror`-image-tag JS-bootstrap trick as `glance-homeassistant` (Glance mounts widget HTML via `element.innerHTML`, which makes `<script>` tags inert — `onerror`/`onload` content attributes are not).

**Tech Stack:** Go 1.23, `net/http` stdlib only (no new third-party dependencies), `gopkg.in/yaml.v3` (already a dependency).

## Global Constraints

- Every new/changed file follows this repo's existing conventions exactly: `New(baseURL, ...)` constructors, context-scoped methods, `ImageResult{Body, ContentType, StatusCode}` for image proxying, TDD RED→GREEN→REFACTOR per step, `gofmt -l .`, `go vet ./...`, `go test ./...` all clean before every commit.
- Radarr and Sonarr authenticate via an `X-Api-Key` header (their documented v3 API convention) — not a query-string `apikey` param.
- Radarr/Sonarr's JSON field names below (`movieId`, `sizeleft`, `records`, etc.) reflect their documented, version-stable v3 API contract. If the user's live instances return different shapes, Task 7's deployment smoke-test is where that gets caught and the Task 1/2 parsing structs get adjusted — a live-verification step, not a guess left unresolved, exactly like the Jellyfin Play-URL verification in Task 3.
- Radarr/Sonarr URLs never get a `public_url` counterpart — the browser never talks to them directly, only this service's poller and image proxy do (over the same internal Docker network this container already reaches `jellyfin:8096` through).
- The existing `validItemID` regex (`^[0-9a-fA-F-]+$`) is reused unchanged for Radarr/Sonarr's plain-integer IDs (digits are a subset of hex characters) — do not write a second regex.
- `/live.json` and `/image/` must be dual-registered at both the bare path and the `{public_url}`-prefixed path, exactly like the existing `/image/` registration — this exact bug class (browser-relative URLs breaking behind a path-prefixed reverse proxy) has already been hit and fixed twice in this codebase's history; do not reintroduce it.
- No code anywhere calls qBittorrent or Prowlarr directly — Radarr's/Sonarr's own `/api/v3/queue` already reports download-client progress.
- Client-side live updates use the `onerror`-image-tag bootstrap trick (see `glance-homeassistant/internal/render/template.go`'s `bootstrapScript` + `RenderWidget`'s `<img onerror=...>` line), never a `<script>` tag.

---

### Task 1: Radarr client

**Files:**
- Create: `internal/radarr/client.go`
- Test: `internal/radarr/client_test.go`

**Interfaces:**
- Consumes: nothing new (stdlib only).
- Produces: `radarr.New(baseURL, apiKey string) *Client`; `(*Client).FetchQueue(ctx) ([]QueueItem, error)`; `(*Client).FetchMissing(ctx) ([]MissingItem, error)`; `(*Client).FetchPoster(ctx, movieID string) (*ImageResult, error)`; `QueueItem{MovieID int, Title string, Size, SizeLeft int64}`; `MissingItem{MovieID int, Title string}`; `ImageResult{Body io.ReadCloser, ContentType string, StatusCode int}`. Task 6 (poller) and Task 7 (main.go) consume all of these.

- [ ] **Step 1: Write the failing tests**

```go
// internal/radarr/client_test.go
package radarr

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchQueue_ParsesItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/queue" {
			t.Errorf("path = %s, want /api/v3/queue", r.URL.Path)
		}
		if got := r.Header.Get("X-Api-Key"); got != "test-key" {
			t.Errorf("X-Api-Key = %q, want test-key", got)
		}
		if got := r.URL.Query().Get("includeMovie"); got != "true" {
			t.Errorf("includeMovie = %q, want true", got)
		}
		w.Write([]byte(`{"records":[
			{"movieId":123,"size":1000,"sizeleft":250,"movie":{"title":"Some Movie"}}
		]}`))
	}))
	defer server.Close()

	client := New(server.URL, "test-key")
	items, err := client.FetchQueue(context.Background())
	if err != nil {
		t.Fatalf("FetchQueue: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	want := QueueItem{MovieID: 123, Title: "Some Movie", Size: 1000, SizeLeft: 250}
	if items[0] != want {
		t.Errorf("items[0] = %+v, want %+v", items[0], want)
	}
}

func TestFetchQueue_EmptyQueue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"records":[]}`))
	}))
	defer server.Close()

	client := New(server.URL, "test-key")
	items, err := client.FetchQueue(context.Background())
	if err != nil {
		t.Fatalf("FetchQueue: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestFetchQueue_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(server.URL, "test-key")
	_, err := client.FetchQueue(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want it to mention status 500", err)
	}
}

func TestFetchMissing_ParsesItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/wanted/missing" {
			t.Errorf("path = %s, want /api/v3/wanted/missing", r.URL.Path)
		}
		if got := r.URL.Query().Get("monitored"); got != "true" {
			t.Errorf("monitored = %q, want true", got)
		}
		w.Write([]byte(`{"records":[{"id":456,"title":"Another Movie"}]}`))
	}))
	defer server.Close()

	client := New(server.URL, "test-key")
	items, err := client.FetchMissing(context.Background())
	if err != nil {
		t.Fatalf("FetchMissing: %v", err)
	}
	want := []MissingItem{{MovieID: 456, Title: "Another Movie"}}
	if len(items) != 1 || items[0] != want[0] {
		t.Errorf("items = %+v, want %+v", items, want)
	}
}

func TestFetchPoster_StreamsBodyAndContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/MediaCover/123/poster.jpg" {
			t.Errorf("path = %s, want /api/v3/MediaCover/123/poster.jpg", r.URL.Path)
		}
		if got := r.Header.Get("X-Api-Key"); got != "test-key" {
			t.Errorf("X-Api-Key = %q, want test-key", got)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("fake-poster-bytes"))
	}))
	defer server.Close()

	client := New(server.URL, "test-key")
	result, err := client.FetchPoster(context.Background(), "123")
	if err != nil {
		t.Fatalf("FetchPoster: %v", err)
	}
	defer result.Body.Close()

	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}
	if result.ContentType != "image/jpeg" {
		t.Errorf("ContentType = %q, want image/jpeg", result.ContentType)
	}
	body, _ := io.ReadAll(result.Body)
	if string(body) != "fake-poster-bytes" {
		t.Errorf("body = %q, want fake-poster-bytes", body)
	}
}

func TestFetchPoster_NonOKStatusReturnsStatusCodeNotError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := New(server.URL, "test-key")
	result, err := client.FetchPoster(context.Background(), "999")
	if err != nil {
		t.Fatalf("FetchPoster: %v", err)
	}
	defer result.Body.Close()
	if result.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", result.StatusCode)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/radarr/...`
Expected: FAIL — `radarr` package / `New` / `Client` undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/radarr/client.go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/radarr/...`
Expected: PASS, all 6 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/radarr
git commit -m "Add Radarr client for queue, wanted/missing, and poster proxying"
```

---

### Task 2: Sonarr client

**Files:**
- Create: `internal/sonarr/client.go`
- Test: `internal/sonarr/client_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `sonarr.New(baseURL, apiKey string) *Client`; `(*Client).FetchQueue(ctx) ([]QueueItem, error)`; `(*Client).FetchMissing(ctx) ([]MissingItem, error)`; `(*Client).FetchPoster(ctx, seriesID string) (*ImageResult, error)`; `QueueItem{SeriesID int, Title string, Size, SizeLeft int64}`; `MissingItem{SeriesID int, Title string}`; `ImageResult` (same shape as `radarr.ImageResult`). Consumed by Task 6 (poller) and Task 7 (main.go). Queue/missing are per-episode in Sonarr's API but this client surfaces the *series* title/ID for each — episode-level detail isn't needed since Task 6 aggregates to one card per series.

- [ ] **Step 1: Write the failing tests**

```go
// internal/sonarr/client_test.go
package sonarr

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchQueue_ParsesItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/queue" {
			t.Errorf("path = %s, want /api/v3/queue", r.URL.Path)
		}
		if got := r.Header.Get("X-Api-Key"); got != "test-key" {
			t.Errorf("X-Api-Key = %q, want test-key", got)
		}
		if got := r.URL.Query().Get("includeSeries"); got != "true" {
			t.Errorf("includeSeries = %q, want true", got)
		}
		w.Write([]byte(`{"records":[
			{"seriesId":55,"episodeId":901,"size":2000,"sizeleft":500,"series":{"title":"Some Show"}}
		]}`))
	}))
	defer server.Close()

	client := New(server.URL, "test-key")
	items, err := client.FetchQueue(context.Background())
	if err != nil {
		t.Fatalf("FetchQueue: %v", err)
	}
	want := QueueItem{SeriesID: 55, Title: "Some Show", Size: 2000, SizeLeft: 500}
	if len(items) != 1 || items[0] != want {
		t.Errorf("items = %+v, want [%+v]", items, want)
	}
}

func TestFetchQueue_MultipleEpisodesSameSeries(t *testing.T) {
	// Sonarr's queue is per-episode: two downloading episodes of the same
	// show produce two QueueItem entries with the same SeriesID. Task 6's
	// poller — not this client — is responsible for picking one to show.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"records":[
			{"seriesId":55,"episodeId":901,"size":2000,"sizeleft":500,"series":{"title":"Some Show"}},
			{"seriesId":55,"episodeId":902,"size":2000,"sizeleft":1800,"series":{"title":"Some Show"}}
		]}`))
	}))
	defer server.Close()

	client := New(server.URL, "test-key")
	items, err := client.FetchQueue(context.Background())
	if err != nil {
		t.Fatalf("FetchQueue: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
}

func TestFetchQueue_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(server.URL, "test-key")
	_, err := client.FetchQueue(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want it to mention status 500", err)
	}
}

func TestFetchMissing_ParsesItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/wanted/missing" {
			t.Errorf("path = %s, want /api/v3/wanted/missing", r.URL.Path)
		}
		if got := r.URL.Query().Get("includeSeries"); got != "true" {
			t.Errorf("includeSeries = %q, want true", got)
		}
		w.Write([]byte(`{"records":[{"id":901,"seriesId":55,"series":{"title":"Some Show"}}]}`))
	}))
	defer server.Close()

	client := New(server.URL, "test-key")
	items, err := client.FetchMissing(context.Background())
	if err != nil {
		t.Fatalf("FetchMissing: %v", err)
	}
	want := []MissingItem{{SeriesID: 55, Title: "Some Show"}}
	if len(items) != 1 || items[0] != want[0] {
		t.Errorf("items = %+v, want %+v", items, want)
	}
}

func TestFetchPoster_StreamsBodyAndContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/MediaCover/55/poster.jpg" {
			t.Errorf("path = %s, want /api/v3/MediaCover/55/poster.jpg", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("fake-poster-bytes"))
	}))
	defer server.Close()

	client := New(server.URL, "test-key")
	result, err := client.FetchPoster(context.Background(), "55")
	if err != nil {
		t.Fatalf("FetchPoster: %v", err)
	}
	defer result.Body.Close()
	body, _ := io.ReadAll(result.Body)
	if string(body) != "fake-poster-bytes" {
		t.Errorf("body = %q, want fake-poster-bytes", body)
	}
}

func TestFetchPoster_NonOKStatusReturnsStatusCodeNotError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := New(server.URL, "test-key")
	result, err := client.FetchPoster(context.Background(), "999")
	if err != nil {
		t.Fatalf("FetchPoster: %v", err)
	}
	defer result.Body.Close()
	if result.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", result.StatusCode)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sonarr/...`
Expected: FAIL — package/`New`/`Client` undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/sonarr/client.go
package sonarr

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
	SeriesID int
	Title    string
	Size     int64
	SizeLeft int64
}

func (c *Client) FetchQueue(ctx context.Context) ([]QueueItem, error) {
	req, err := c.newRequest(ctx, "/api/v3/queue", map[string]string{
		"includeSeries": "true",
		"pageSize":      "200",
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
			SeriesID int   `json:"seriesId"`
			Size     int64 `json:"size"`
			SizeLeft int64 `json:"sizeleft"`
			Series   struct {
				Title string `json:"title"`
			} `json:"series"`
		} `json:"records"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse queue response: %w", err)
	}

	items := make([]QueueItem, len(raw.Records))
	for i, r := range raw.Records {
		items[i] = QueueItem{SeriesID: r.SeriesID, Title: r.Series.Title, Size: r.Size, SizeLeft: r.SizeLeft}
	}
	return items, nil
}

type MissingItem struct {
	SeriesID int
	Title    string
}

func (c *Client) FetchMissing(ctx context.Context) ([]MissingItem, error) {
	req, err := c.newRequest(ctx, "/api/v3/wanted/missing", map[string]string{
		"monitored":     "true",
		"includeSeries": "true",
		"pageSize":      "200",
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
			SeriesID int `json:"seriesId"`
			Series   struct {
				Title string `json:"title"`
			} `json:"series"`
		} `json:"records"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse wanted/missing response: %w", err)
	}

	items := make([]MissingItem, len(raw.Records))
	for i, r := range raw.Records {
		items[i] = MissingItem{SeriesID: r.SeriesID, Title: r.Series.Title}
	}
	return items, nil
}

type ImageResult struct {
	Body        io.ReadCloser
	ContentType string
	StatusCode  int
}

// FetchPoster streams a series' poster image from Sonarr. The caller owns
// Body and must close it. A non-200 StatusCode is not treated as an error —
// mirrors jellyfin.Client.FetchImage's contract.
func (c *Client) FetchPoster(ctx context.Context, seriesID string) (*ImageResult, error) {
	u := fmt.Sprintf("%s/api/v3/MediaCover/%s/poster.jpg", c.BaseURL, seriesID)
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sonarr/...`
Expected: PASS, all 6 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/sonarr
git commit -m "Add Sonarr client for queue, wanted/missing, and poster proxying"
```

---

### Task 3: Jellyfin server ID (for the Play deep link)

**Files:**
- Modify: `internal/jellyfin/client.go`
- Test: `internal/jellyfin/client_test.go`

**Interfaces:**
- Produces: `(*Client).FetchServerID(ctx) (string, error)`. Consumed by Task 7's `app.fetchServerIDCached`.

- [ ] **Step 1: Write the failing test**

```go
// Append to internal/jellyfin/client_test.go

func TestFetchServerID_ParsesID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/System/Info/Public" {
			t.Errorf("path = %s, want /System/Info/Public", r.URL.Path)
		}
		fmt.Fprint(w, `{"Id":"abc-server-id"}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token", "test-user")
	id, err := client.FetchServerID(context.Background())
	if err != nil {
		t.Fatalf("FetchServerID: %v", err)
	}
	if id != "abc-server-id" {
		t.Errorf("id = %q, want abc-server-id", id)
	}
}

func TestFetchServerID_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(server.URL, "test-token", "test-user")
	_, err := client.FetchServerID(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/jellyfin/... -run TestFetchServerID`
Expected: FAIL — `FetchServerID` undefined.

- [ ] **Step 3: Write the implementation**

Append to `internal/jellyfin/client.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/jellyfin/...`
Expected: PASS, all tests including the two new ones.

- [ ] **Step 5: Commit**

```bash
git add internal/jellyfin/client.go internal/jellyfin/client_test.go
git commit -m "Add FetchServerID for Jellyfin playback deep links"
```

---

### Task 4: Config — Radarr/Sonarr settings

**Files:**
- Modify: `config.go`
- Test: `config_test.go`

**Interfaces:**
- Produces: `Config.Radarr RadarrConfig`, `Config.Sonarr SonarrConfig`, `Config.DownloadingLimit int`; `RadarrConfig{URL, Token string}`; `SonarrConfig{URL, Token string}`. Consumed by Task 7's `newApp`.

- [ ] **Step 1: Write the failing tests**

Append to `config_test.go`:

```go
func TestLoadConfig_RadarrSonarrDefaults(t *testing.T) {
	path := writeTempConfig(t, `
jellyfin:
  url: http://jellyfin:8096
  token: test-token
  user_id: test-user
  public_url: https://jellyfin.example.com
radarr:
  url: http://radarr:7878
  token: radarr-key
sonarr:
  url: http://sonarr:8989
  token: sonarr-key
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Radarr.URL != "http://radarr:7878" || cfg.Radarr.Token != "radarr-key" {
		t.Errorf("Radarr = %+v, want {http://radarr:7878 radarr-key}", cfg.Radarr)
	}
	if cfg.Sonarr.URL != "http://sonarr:8989" || cfg.Sonarr.Token != "sonarr-key" {
		t.Errorf("Sonarr = %+v, want {http://sonarr:8989 sonarr-key}", cfg.Sonarr)
	}
	if cfg.DownloadingLimit != 12 {
		t.Errorf("DownloadingLimit = %d, want 12 (default)", cfg.DownloadingLimit)
	}
}

func TestLoadConfig_RadarrSonarrEnvOverrides(t *testing.T) {
	setEnv(t, "JELLYFIN_URL", "http://jellyfin:8096")
	setEnv(t, "JELLYFIN_TOKEN", "t")
	setEnv(t, "JELLYFIN_USER_ID", "u")
	setEnv(t, "JELLYFIN_PUBLIC_URL", "https://jf.example.com")
	setEnv(t, "RADARR_URL", "http://radarr.internal:7878")
	setEnv(t, "RADARR_TOKEN", "env-radarr-key")
	setEnv(t, "SONARR_URL", "http://sonarr.internal:8989")
	setEnv(t, "SONARR_TOKEN", "env-sonarr-key")
	setEnv(t, "DOWNLOADING_LIMIT", "20")

	path := writeTempConfig(t, `title: ignored`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Radarr.URL != "http://radarr.internal:7878" {
		t.Errorf("Radarr.URL = %q, want env override", cfg.Radarr.URL)
	}
	if cfg.Radarr.Token != "env-radarr-key" {
		t.Errorf("Radarr.Token = %q, want env override", cfg.Radarr.Token)
	}
	if cfg.Sonarr.URL != "http://sonarr.internal:8989" {
		t.Errorf("Sonarr.URL = %q, want env override", cfg.Sonarr.URL)
	}
	if cfg.Sonarr.Token != "env-sonarr-key" {
		t.Errorf("Sonarr.Token = %q, want env override", cfg.Sonarr.Token)
	}
	if cfg.DownloadingLimit != 20 {
		t.Errorf("DownloadingLimit = %d, want 20", cfg.DownloadingLimit)
	}
}

func TestLoadConfig_MissingRadarrSonarrFieldsError(t *testing.T) {
	base := "jellyfin:\n  url: u\n  token: t\n  user_id: u\n  public_url: p\n"
	cases := []struct {
		name   string
		yaml   string
		wantIn string
	}{
		{"missing radarr.url", base + "radarr:\n  token: t\nsonarr:\n  url: u\n  token: t", "radarr.url"},
		{"missing radarr.token", base + "radarr:\n  url: u\nsonarr:\n  url: u\n  token: t", "radarr.token"},
		{"missing sonarr.url", base + "radarr:\n  url: u\n  token: t\nsonarr:\n  token: t", "sonarr.url"},
		{"missing sonarr.token", base + "radarr:\n  url: u\n  token: t\nsonarr:\n  url: u", "sonarr.token"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeTempConfig(t, c.yaml)
			_, err := LoadConfig(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), c.wantIn) {
				t.Errorf("error = %v, want it to mention %q", err, c.wantIn)
			}
		})
	}
}

func TestLoadConfig_InvalidDownloadingLimitEnvErrors(t *testing.T) {
	setEnv(t, "DOWNLOADING_LIMIT", "not-a-number")
	path := writeTempConfig(t, `
jellyfin:
  url: http://jellyfin:8096
  token: test-token
  user_id: test-user
  public_url: https://jellyfin.example.com
radarr:
  url: http://radarr:7878
  token: radarr-key
sonarr:
  url: http://sonarr:8989
  token: sonarr-key
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid DOWNLOADING_LIMIT, got nil")
	}
}
```

Note: `TestLoadConfig_MissingRequiredFieldsError` (existing) constructs YAML without a `radarr`/`sonarr` block at all, so it will now also fail on the new required-field checks before reaching its own assertion. Update its four cases (`writeTempConfig` bodies) in Step 3 below to append a valid `radarr:`/`sonarr:` block to each, so each case isolates exactly one missing field again.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test .`
Expected: FAIL — `cfg.Radarr`/`cfg.Sonarr`/`cfg.DownloadingLimit` undefined (compile error), and the four existing `TestLoadConfig_MissingRequiredFieldsError` cases fail once compiling since they don't yet expect the new required fields.

- [ ] **Step 3: Write the implementation**

In `config.go`, update `Config`:

```go
type Config struct {
	Jellyfin JellyfinConfig `yaml:"jellyfin"`
	Radarr   RadarrConfig   `yaml:"radarr"`
	Sonarr   SonarrConfig   `yaml:"sonarr"`
	// PublicURL is THIS service's own path/origin, reachable from the
	// browser — used to prefix the poster <img src="..."> URLs so they
	// reach this container through a reverse proxy, exactly like
	// jellyfin.public_url is Jellyfin's own browser-facing URL for
	// click-through links. Not required: "" means this service is served
	// from the site root.
	PublicURL        string `yaml:"public_url"`
	Title             string `yaml:"title"`
	Limit             int    `yaml:"limit"`
	DownloadingLimit  int    `yaml:"downloading_limit"`
}

type JellyfinConfig struct {
	URL       string `yaml:"url"`
	Token     string `yaml:"token"`
	UserID    string `yaml:"user_id"`
	PublicURL string `yaml:"public_url"`
}

// RadarrConfig and SonarrConfig are reachable only from this container, not
// the browser — unlike Jellyfin, nothing here needs a public_url
// counterpart (see the plan's Global Constraints).
type RadarrConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

type SonarrConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}
```

In `LoadConfig`, after the existing `cfg.Limit == 0` default block, add:

```go
	if cfg.DownloadingLimit == 0 {
		cfg.DownloadingLimit = 12
	}
```

After the existing `cfg.Jellyfin.PublicURL` required check, add:

```go
	if cfg.Radarr.URL == "" {
		return nil, fmt.Errorf("radarr.url is required")
	}
	if cfg.Radarr.Token == "" {
		return nil, fmt.Errorf("radarr.token is required")
	}
	if cfg.Sonarr.URL == "" {
		return nil, fmt.Errorf("sonarr.url is required")
	}
	if cfg.Sonarr.Token == "" {
		return nil, fmt.Errorf("sonarr.token is required")
	}
```

After the existing `cfg.Limit < 0` check, add:

```go
	if cfg.DownloadingLimit < 0 {
		return nil, fmt.Errorf("downloading_limit must not be negative, got %d", cfg.DownloadingLimit)
	}
```

In `applyEnvOverrides`, after the existing `JELLYFIN_PUBLIC_URL` block, add:

```go
	if v, ok := lookupNonEmptyEnv("RADARR_URL"); ok {
		cfg.Radarr.URL = v
	}
	if v, ok := lookupNonEmptyEnv("RADARR_TOKEN"); ok {
		cfg.Radarr.Token = v
	}
	if v, ok := lookupNonEmptyEnv("SONARR_URL"); ok {
		cfg.Sonarr.URL = v
	}
	if v, ok := lookupNonEmptyEnv("SONARR_TOKEN"); ok {
		cfg.Sonarr.Token = v
	}
```

After the existing `LIMIT` env block, add:

```go
	if v, ok := lookupNonEmptyEnv("DOWNLOADING_LIMIT"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("env DOWNLOADING_LIMIT=%q is not a valid integer: %w", v, err)
		}
		cfg.DownloadingLimit = n
	}
```

In `config_test.go`, update the four `TestLoadConfig_MissingRequiredFieldsError` cases to include a valid `radarr:`/`sonarr:` block so each isolates one missing field:

```go
		{"missing url", "jellyfin:\n  token: t\n  user_id: u\n  public_url: p\nradarr:\n  url: u\n  token: t\nsonarr:\n  url: u\n  token: t", "jellyfin.url"},
		{"missing token", "jellyfin:\n  url: u\n  user_id: u\n  public_url: p\nradarr:\n  url: u\n  token: t\nsonarr:\n  url: u\n  token: t", "jellyfin.token"},
		{"missing user_id", "jellyfin:\n  url: u\n  token: t\n  public_url: p\nradarr:\n  url: u\n  token: t\nsonarr:\n  url: u\n  token: t", "jellyfin.user_id"},
		{"missing public_url", "jellyfin:\n  url: u\n  token: t\n  user_id: u\nradarr:\n  url: u\n  token: t\nsonarr:\n  url: u\n  token: t", "jellyfin.public_url"},
```

Also update `TestLoadConfig_Defaults` and `TestLoadConfig_EnvOverrides`'s YAML/env fixtures to include valid `radarr`/`sonarr` config (a `radarr:`/`sonarr:` block in the former, `RADARR_URL`/`RADARR_TOKEN`/`SONARR_URL`/`SONARR_TOKEN` env vars set via `setEnv` in the latter), since `LoadConfig` now errors without them.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test .`
Expected: PASS, all tests including the new ones.

- [ ] **Step 5: Commit**

```bash
git add config.go config_test.go
git commit -m "Add Radarr/Sonarr config fields and downloading_limit"
```

---

### Task 5: Render — Play button, Downloading section, live JSON

**Files:**
- Modify: `internal/render/grid.go`
- Create: `internal/render/downloading.go`
- Modify: `internal/render/grid_test.go`
- Create: `internal/render/downloading_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `CardView.PlayHref string` (new field); `WidgetData.Downloading []DownloadCardView`, `WidgetData.LiveURL string`, `WidgetData.PollIntervalMS int` (new fields); `DownloadCardView{ItemID, Title, Poster, Status string, Percent int}`; `RenderDownloadingLive(items []DownloadCardView) ([]byte, error)`. Consumed by Task 7 (`widgetHandler`/`liveHandler`).

- [ ] **Step 1: Write the failing tests**

Append to `internal/render/grid_test.go`:

```go
func TestRenderWidget_CardHasDistinctPlayLink(t *testing.T) {
	html := RenderWidget(WidgetData{Cards: []CardView{
		{
			Title:    "The Sheep Detectives",
			ImageSrc: "/image/jellyfin/abc123",
			Href:     "https://jellyfin.example/web/#/details?id=abc123",
			PlayHref: "https://jellyfin.example/web/#/video?id=abc123&serverId=srv1",
		},
	}})
	if !contains(html, `href="https://jellyfin.example/web/#/video?id=abc123&amp;serverId=srv1"`) {
		t.Errorf("html missing play href: %q", html)
	}
	// Both the details link and the play link must exist as distinct
	// elements — nesting an <a> inside another <a> is invalid HTML.
	if !contains(html, `href="https://jellyfin.example/web/#/details?id=abc123"`) {
		t.Errorf("html missing details href: %q", html)
	}
}

func TestRenderWidget_DownloadingSectionOmittedWhenEmpty(t *testing.T) {
	html := RenderWidget(WidgetData{Cards: nil, Downloading: nil})
	if contains(html, "jf-dl-card") {
		t.Errorf("html has a downloading card when Downloading is empty: %q", html)
	}
}

func TestRenderWidget_RendersDownloadingCards(t *testing.T) {
	html := RenderWidget(WidgetData{Downloading: []DownloadCardView{
		{ItemID: "radarr-1", Title: "Downloading Movie", Poster: "/image/radarr/1", Status: "downloading", Percent: 42},
		{ItemID: "sonarr-2", Title: "Searching Show", Poster: "/image/sonarr/2", Status: "searching"},
	}})
	if count(html, "jf-dl-card") != 2 {
		t.Errorf("downloading card count wrong: %q", html)
	}
	if !contains(html, `data-item-id="radarr-1"`) {
		t.Errorf("html missing data-item-id for downloading card: %q", html)
	}
	if !contains(html, `data-status="downloading"`) {
		t.Errorf("html missing data-status=downloading: %q", html)
	}
	if !contains(html, "42%") {
		t.Errorf("html missing percentage text: %q", html)
	}
	if !contains(html, `data-item-id="sonarr-2"`) || !contains(html, `data-status="searching"`) {
		t.Errorf("html missing searching card markup: %q", html)
	}
	if !contains(html, "Searching") {
		t.Errorf("html missing 'Searching' label: %q", html)
	}
}

func TestRenderWidget_IncludesLiveBootstrapAttributes(t *testing.T) {
	html := RenderWidget(WidgetData{LiveURL: "/jellyfin-widget/live.json", PollIntervalMS: 12000})
	if !contains(html, `data-live-url="/jellyfin-widget/live.json"`) {
		t.Errorf("html missing data-live-url: %q", html)
	}
	if !contains(html, `data-poll-ms="12000"`) {
		t.Errorf("html missing data-poll-ms: %q", html)
	}
	if !contains(html, "onerror=") {
		t.Errorf("html missing onerror bootstrap trick: %q", html)
	}
}
```

Create `internal/render/downloading_test.go`:

```go
package render

import (
	"encoding/json"
	"testing"
)

func TestRenderDownloadingLive_SerializesItems(t *testing.T) {
	body, err := RenderDownloadingLive([]DownloadCardView{
		{ItemID: "radarr-1", Title: "Ignored In Live Payload", Status: "downloading", Percent: 42},
		{ItemID: "sonarr-2", Status: "searching"},
	})
	if err != nil {
		t.Fatalf("RenderDownloadingLive: %v", err)
	}

	var payload struct {
		Items []struct {
			ItemID  string `json:"item_id"`
			Status  string `json:"status"`
			Percent int    `json:"percent"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(payload.Items))
	}
	if payload.Items[0].ItemID != "radarr-1" || payload.Items[0].Status != "downloading" || payload.Items[0].Percent != 42 {
		t.Errorf("items[0] = %+v, want {radarr-1 downloading 42}", payload.Items[0])
	}
	if payload.Items[1].ItemID != "sonarr-2" || payload.Items[1].Status != "searching" {
		t.Errorf("items[1] = %+v, want {sonarr-2 searching 0}", payload.Items[1])
	}
}

func TestRenderDownloadingLive_EmptyItemsSerializesEmptyArray(t *testing.T) {
	body, err := RenderDownloadingLive(nil)
	if err != nil {
		t.Fatalf("RenderDownloadingLive: %v", err)
	}
	if string(body) != `{"items":[]}` {
		t.Errorf("body = %s, want {\"items\":[]} (not null, so client-side .forEach never sees null)", body)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/render/...`
Expected: FAIL — `CardView.PlayHref`, `WidgetData.Downloading`/`LiveURL`/`PollIntervalMS`, `DownloadCardView`, `RenderDownloadingLive` all undefined.

- [ ] **Step 3: Write the implementation**

Replace `internal/render/grid.go` in full:

```go
package render

import (
	"fmt"
	"html"
	"strings"
)

type CardView struct {
	Title    string
	ImageSrc string
	Href     string
	PlayHref string
}

type WidgetData struct {
	Cards          []CardView
	Downloading    []DownloadCardView
	LiveURL        string
	PollIntervalMS int
}

func styleBlock() string {
	return `<style>
	.jf-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(100px,1fr));gap:10px}
	.jf-card{position:relative;display:block}
	.jf-card-link{display:block;color:inherit;text-decoration:none}
	.jf-poster{width:100%;aspect-ratio:2/3;object-fit:cover;border-radius:6px;display:block;background:var(--color-widget-background-highlight)}
	.jf-title{font-size:11px;color:var(--color-text-highlight);margin-top:4px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
	.jf-empty{color:var(--color-text-subdue);font-size:.85em;padding:8px 0}
	.jf-unavailable{color:var(--color-text-subdue);padding:12px 0}
	.jf-play-btn{position:absolute;top:6px;right:6px;display:flex;align-items:center;justify-content:center;width:24px;height:24px;border-radius:50%;background:rgba(0,0,0,.6);color:#fff;text-decoration:none;font-size:11px;line-height:1}
	.jf-play-btn:hover{background:rgba(0,0,0,.8)}
	.jf-section-label{font-size:11px;text-transform:uppercase;letter-spacing:.04em;color:var(--color-text-subdue);margin:14px 0 6px}
	.jf-dl-card{display:block}
	.jf-dl-status{margin-top:4px}
	.jf-dl-bar{height:4px;border-radius:2px;background:var(--color-widget-background-highlight);overflow:hidden}
	.jf-dl-status[data-status="searching"] .jf-dl-bar{display:none}
	.jf-dl-fill{height:100%;background:var(--color-primary)}
	.jf-dl-pct{font-size:10px;color:var(--color-text-subdue);margin-top:2px}
</style>`
}

// bootstrapScript runs via an onerror attribute (see RenderWidget) because
// Glance mounts extension widget HTML with element.innerHTML, and <script>
// elements inserted that way are inert per the HTML spec — onerror/onload
// content attributes are not. It only ever patches data-* attributes and
// text content on cards that already exist in the initial render; it never
// adds or removes cards (that only happens on Glance's next full-page
// fetch).
const bootstrapScript = `(function(img){var root=img.closest('.jf-widget');if(!root)return;var url=root.dataset.liveUrl;var interval=parseInt(root.dataset.pollMs,10)||12000;function applyState(data){(data.items||[]).forEach(function(item){var card=root.querySelector('.jf-dl-card[data-item-id="'+CSS.escape(item.item_id)+'"]');if(!card)return;var status=card.querySelector('.jf-dl-status');if(!status)return;status.dataset.status=item.status;var fill=status.querySelector('.jf-dl-fill');if(fill)fill.style.width=item.percent+'%';var pct=status.querySelector('.jf-dl-pct');if(pct)pct.textContent=item.status==='downloading'?item.percent+'%':'Searching…';});}function poll(){fetch(url,{cache:'no-store'}).then(function(r){return r.ok?r.json():null;}).then(function(data){if(data)applyState(data);}).catch(function(){});}setInterval(poll,interval);poll();})(this)`

func RenderWidget(data WidgetData) string {
	var b strings.Builder
	b.WriteString(styleBlock())

	fmt.Fprintf(&b, `<div class="jf-widget" data-live-url="%s" data-poll-ms="%d">`,
		html.EscapeString(data.LiveURL), data.PollIntervalMS)

	if len(data.Cards) == 0 {
		b.WriteString(`<div class="jf-empty">no recently added items found</div>`)
	} else {
		b.WriteString(`<div class="jf-grid">`)
		for _, c := range data.Cards {
			b.WriteString(renderCard(c))
		}
		b.WriteString(`</div>`)
	}

	if len(data.Downloading) > 0 {
		b.WriteString(renderDownloadingSection(data.Downloading))
	}

	fmt.Fprintf(&b, `<img src="x" alt="" style="display:none;width:0;height:0" onerror="%s">`, html.EscapeString(bootstrapScript))
	b.WriteString(`</div>`)
	return b.String()
}

func renderCard(c CardView) string {
	var b strings.Builder
	b.WriteString(`<div class="jf-card">`)
	fmt.Fprintf(&b,
		`<a class="jf-card-link" href="%s" target="_blank" rel="noopener"><img class="jf-poster" src="%s" alt="%s" loading="lazy"><div class="jf-title">%s</div></a>`,
		html.EscapeString(c.Href), html.EscapeString(c.ImageSrc), html.EscapeString(c.Title), html.EscapeString(c.Title),
	)
	if c.PlayHref != "" {
		fmt.Fprintf(&b, `<a class="jf-play-btn" href="%s" target="_blank" rel="noopener" aria-label="Play">&#9654;</a>`, html.EscapeString(c.PlayHref))
	}
	b.WriteString(`</div>`)
	return b.String()
}

func RenderUnavailable() string {
	return styleBlock() + `<div class="jf-unavailable">Jellyfin unavailable</div>`
}
```

Create `internal/render/downloading.go`:

```go
package render

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
)

type DownloadCardView struct {
	ItemID  string
	Title   string
	Poster  string
	Status  string // "searching" or "downloading"
	Percent int
}

func renderDownloadingSection(items []DownloadCardView) string {
	var b strings.Builder
	b.WriteString(`<div class="jf-section-label">Downloading</div><div class="jf-grid">`)
	for _, item := range items {
		pctText := "Searching&hellip;"
		width := 0
		if item.Status == "downloading" {
			pctText = fmt.Sprintf("%d%%", item.Percent)
			width = item.Percent
		}
		fmt.Fprintf(&b,
			`<div class="jf-dl-card" data-item-id="%s"><img class="jf-poster" src="%s" alt="%s" loading="lazy"><div class="jf-title">%s</div><div class="jf-dl-status" data-status="%s"><div class="jf-dl-bar"><div class="jf-dl-fill" style="width:%d%%"></div></div><div class="jf-dl-pct">%s</div></div></div>`,
			html.EscapeString(item.ItemID), html.EscapeString(item.Poster), html.EscapeString(item.Title),
			html.EscapeString(item.Title), html.EscapeString(item.Status), width, pctText,
		)
	}
	b.WriteString(`</div>`)
	return b.String()
}

type liveItem struct {
	ItemID  string `json:"item_id"`
	Status  string `json:"status"`
	Percent int    `json:"percent"`
}

type livePayload struct {
	Items []liveItem `json:"items"`
}

// RenderDownloadingLive builds the /live.json payload from the same
// DownloadCardView data used to render the widget, so live updates always
// match one source of truth.
func RenderDownloadingLive(items []DownloadCardView) ([]byte, error) {
	payload := livePayload{Items: []liveItem{}}
	for _, it := range items {
		payload.Items = append(payload.Items, liveItem{ItemID: it.ItemID, Status: it.Status, Percent: it.Percent})
	}
	return json.Marshal(payload)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/render/...`
Expected: PASS, all tests including the existing ones (`TestRenderWidget_RendersOneCardPerItem` etc. use substring `contains()`/`count()` checks against `class="jf-card"` and `href="..."`, which still match the restructured markup).

- [ ] **Step 5: Commit**

```bash
git add internal/render
git commit -m "Add Play button, Downloading section, and live JSON payload to render package"
```

---

### Task 6: Download poller

**Files:**
- Create: `poller.go`
- Test: `poller_test.go`

**Interfaces:**
- Consumes: `radarr.Client` (Task 1), `sonarr.Client` (Task 2), `render.DownloadCardView` (Task 5).
- Produces: `newDownloadPoller(radarrClient *radarr.Client, sonarrClient *sonarr.Client, limit int) *downloadPoller`; `(*downloadPoller).Start(ctx context.Context, interval time.Duration)`; `(*downloadPoller).Snapshot() []render.DownloadCardView`. Consumed by Task 7's `main()`/`widgetHandler`/`liveHandler`.

- [ ] **Step 1: Write the failing tests**

```go
// poller_test.go
package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sidun-av/glance-jellyfin/internal/radarr"
	"github.com/sidun-av/glance-jellyfin/internal/sonarr"
)

func fakeRadarrServer(queueBody, missingBody string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/queue":
			w.Write([]byte(queueBody))
		case "/api/v3/wanted/missing":
			w.Write([]byte(missingBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func fakeSonarrServer(queueBody, missingBody string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/queue":
			w.Write([]byte(queueBody))
		case "/api/v3/wanted/missing":
			w.Write([]byte(missingBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestPoller_MergesRadarrAndSonarr(t *testing.T) {
	rr := fakeRadarrServer(
		`{"records":[{"movieId":1,"size":1000,"sizeleft":500,"movie":{"title":"Downloading Movie"}}]}`,
		`{"records":[]}`,
	)
	defer rr.Close()
	sr := fakeSonarrServer(
		`{"records":[]}`,
		`{"records":[{"seriesId":2,"series":{"title":"Searching Show"}}]}`,
	)
	defer sr.Close()

	p := newDownloadPoller(radarr.New(rr.URL, "k"), sonarr.New(sr.URL, "k"), 12)
	p.poll(context.Background())

	got := p.Snapshot()
	if len(got) != 2 {
		t.Fatalf("len(snapshot) = %d, want 2: %+v", len(got), got)
	}
	// Downloading items sort before searching items.
	if got[0].ItemID != "radarr-1" || got[0].Status != "downloading" || got[0].Percent != 50 {
		t.Errorf("got[0] = %+v, want {radarr-1 ... downloading 50}", got[0])
	}
	if got[1].ItemID != "sonarr-2" || got[1].Status != "searching" {
		t.Errorf("got[1] = %+v, want {sonarr-2 ... searching 0}", got[1])
	}
}

func TestPoller_SonarrAggregatesMultipleEpisodesToHighestProgress(t *testing.T) {
	sr := fakeSonarrServer(
		`{"records":[
			{"seriesId":5,"size":1000,"sizeleft":800,"series":{"title":"Some Show"}},
			{"seriesId":5,"size":1000,"sizeleft":100,"series":{"title":"Some Show"}}
		]}`,
		`{"records":[]}`,
	)
	defer sr.Close()
	rr := fakeRadarrServer(`{"records":[]}`, `{"records":[]}`)
	defer rr.Close()

	p := newDownloadPoller(radarr.New(rr.URL, "k"), sonarr.New(sr.URL, "k"), 12)
	p.poll(context.Background())

	got := p.Snapshot()
	if len(got) != 1 {
		t.Fatalf("len(snapshot) = %d, want 1 (one card per series): %+v", len(got), got)
	}
	if got[0].Percent != 90 {
		t.Errorf("Percent = %d, want 90 (the more-complete episode)", got[0].Percent)
	}
}

func TestPoller_SonarrMissingSeriesAlreadyInQueueStaysDownloading(t *testing.T) {
	sr := fakeSonarrServer(
		`{"records":[{"seriesId":5,"size":1000,"sizeleft":500,"series":{"title":"Some Show"}}]}`,
		`{"records":[{"seriesId":5,"series":{"title":"Some Show"}}]}`,
	)
	defer sr.Close()
	rr := fakeRadarrServer(`{"records":[]}`, `{"records":[]}`)
	defer rr.Close()

	p := newDownloadPoller(radarr.New(rr.URL, "k"), sonarr.New(sr.URL, "k"), 12)
	p.poll(context.Background())

	got := p.Snapshot()
	if len(got) != 1 || got[0].Status != "downloading" {
		t.Fatalf("snapshot = %+v, want one downloading card (queue wins over missing for the same series)", got)
	}
}

func TestPoller_KeepsLastGoodSnapshotOnSourceFailure(t *testing.T) {
	rr := fakeRadarrServer(`{"records":[{"movieId":1,"size":1000,"sizeleft":500,"movie":{"title":"M"}}]}`, `{"records":[]}`)
	defer rr.Close()
	sr := fakeSonarrServer(`{"records":[]}`, `{"records":[]}`)
	defer sr.Close()

	p := newDownloadPoller(radarr.New(rr.URL, "k"), sonarr.New(sr.URL, "k"), 12)
	p.poll(context.Background())
	if len(p.Snapshot()) != 1 {
		t.Fatalf("expected 1 item after first successful poll, got %d", len(p.Snapshot()))
	}

	rr.Close() // radarr now unreachable
	p.poll(context.Background())
	if len(p.Snapshot()) != 1 {
		t.Errorf("expected the last-known-good radarr item to survive a failed poll, got %d items", len(p.Snapshot()))
	}
}

func TestPoller_RespectsLimit(t *testing.T) {
	rr := fakeRadarrServer(
		`{"records":[
			{"movieId":1,"size":1000,"sizeleft":500,"movie":{"title":"A"}},
			{"movieId":2,"size":1000,"sizeleft":500,"movie":{"title":"B"}},
			{"movieId":3,"size":1000,"sizeleft":500,"movie":{"title":"C"}}
		]}`,
		`{"records":[]}`,
	)
	defer rr.Close()
	sr := fakeSonarrServer(`{"records":[]}`, `{"records":[]}`)
	defer sr.Close()

	p := newDownloadPoller(radarr.New(rr.URL, "k"), sonarr.New(sr.URL, "k"), 2)
	p.poll(context.Background())

	if len(p.Snapshot()) != 2 {
		t.Errorf("len(snapshot) = %d, want 2 (limit)", len(p.Snapshot()))
	}
}

func TestPoller_StartPollsImmediatelyThenOnInterval(t *testing.T) {
	rr := fakeRadarrServer(`{"records":[{"movieId":1,"size":1000,"sizeleft":500,"movie":{"title":"M"}}]}`, `{"records":[]}`)
	defer rr.Close()
	sr := fakeSonarrServer(`{"records":[]}`, `{"records":[]}`)
	defer sr.Close()

	p := newDownloadPoller(radarr.New(rr.URL, "k"), sonarr.New(sr.URL, "k"), 12)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx, time.Hour) // long interval — this test only checks the immediate poll

	deadline := time.Now().Add(2 * time.Second)
	for len(p.Snapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(p.Snapshot()) != 1 {
		t.Fatalf("Start did not populate the snapshot immediately: got %d items", len(p.Snapshot()))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test . -run TestPoller`
Expected: FAIL — `newDownloadPoller`/`downloadPoller` undefined.

- [ ] **Step 3: Write the implementation**

```go
// poller.go
package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/sidun-av/glance-jellyfin/internal/radarr"
	"github.com/sidun-av/glance-jellyfin/internal/render"
	"github.com/sidun-av/glance-jellyfin/internal/sonarr"
)

// downloadPoller is the only thing that ever calls Radarr/Sonarr: it polls
// on a fixed interval and caches the result, so /widget and /live.json
// (however many browser tabs are open) only ever read the cached snapshot.
// Each source's cards are cached independently, so a transient failure of
// one source (e.g. Radarr restarting) doesn't blank out the other source's
// still-fresh data.
type downloadPoller struct {
	radarr *radarr.Client
	sonarr *sonarr.Client
	limit  int

	mu          sync.RWMutex
	radarrCards []render.DownloadCardView
	sonarrCards []render.DownloadCardView
}

func newDownloadPoller(radarrClient *radarr.Client, sonarrClient *sonarr.Client, limit int) *downloadPoller {
	return &downloadPoller{radarr: radarrClient, sonarr: sonarrClient, limit: limit}
}

// Start polls once immediately (so Snapshot has data before the first
// request arrives) and then on every tick of interval, until ctx is done.
func (p *downloadPoller) Start(ctx context.Context, interval time.Duration) {
	p.poll(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.poll(ctx)
			}
		}
	}()
}

func (p *downloadPoller) poll(ctx context.Context) {
	if cards, err := fetchRadarrCards(ctx, p.radarr); err != nil {
		log.Printf("radarr unavailable: %v", err)
	} else {
		p.mu.Lock()
		p.radarrCards = cards
		p.mu.Unlock()
	}

	if cards, err := fetchSonarrCards(ctx, p.sonarr); err != nil {
		log.Printf("sonarr unavailable: %v", err)
	} else {
		p.mu.Lock()
		p.sonarrCards = cards
		p.mu.Unlock()
	}
}

func (p *downloadPoller) Snapshot() []render.DownloadCardView {
	p.mu.RLock()
	defer p.mu.RUnlock()

	combined := make([]render.DownloadCardView, 0, len(p.radarrCards)+len(p.sonarrCards))
	combined = append(combined, p.radarrCards...)
	combined = append(combined, p.sonarrCards...)
	sortDownloadCards(combined)
	if len(combined) > p.limit {
		combined = combined[:p.limit]
	}
	return combined
}

func fetchRadarrCards(ctx context.Context, c *radarr.Client) ([]render.DownloadCardView, error) {
	queue, err := c.FetchQueue(ctx)
	if err != nil {
		return nil, err
	}
	missing, err := c.FetchMissing(ctx)
	if err != nil {
		return nil, err
	}

	downloading := make(map[int]render.DownloadCardView, len(queue))
	for _, q := range queue {
		downloading[q.MovieID] = render.DownloadCardView{
			ItemID:  fmt.Sprintf("radarr-%d", q.MovieID),
			Title:   q.Title,
			Poster:  fmt.Sprintf("/image/radarr/%d", q.MovieID),
			Status:  "downloading",
			Percent: percentComplete(q.Size, q.SizeLeft),
		}
	}

	cards := make([]render.DownloadCardView, 0, len(downloading)+len(missing))
	for _, d := range downloading {
		cards = append(cards, d)
	}
	for _, m := range missing {
		if _, alreadyDownloading := downloading[m.MovieID]; alreadyDownloading {
			continue
		}
		cards = append(cards, render.DownloadCardView{
			ItemID: fmt.Sprintf("radarr-%d", m.MovieID),
			Title:  m.Title,
			Poster: fmt.Sprintf("/image/radarr/%d", m.MovieID),
			Status: "searching",
		})
	}
	return cards, nil
}

// fetchSonarrCards aggregates Sonarr's per-episode queue/missing entries to
// one card per series: a series with any queued episode shows that
// episode's progress (the highest-progress one, if several are queued
// simultaneously); otherwise a series with any missing episode shows
// "searching".
func fetchSonarrCards(ctx context.Context, c *sonarr.Client) ([]render.DownloadCardView, error) {
	queue, err := c.FetchQueue(ctx)
	if err != nil {
		return nil, err
	}
	missing, err := c.FetchMissing(ctx)
	if err != nil {
		return nil, err
	}

	type seriesState struct {
		title   string
		percent int
	}
	downloading := make(map[int]seriesState, len(queue))
	for _, q := range queue {
		pct := percentComplete(q.Size, q.SizeLeft)
		if existing, ok := downloading[q.SeriesID]; !ok || pct > existing.percent {
			downloading[q.SeriesID] = seriesState{title: q.Title, percent: pct}
		}
	}

	cards := make([]render.DownloadCardView, 0, len(downloading)+len(missing))
	for id, s := range downloading {
		cards = append(cards, render.DownloadCardView{
			ItemID:  fmt.Sprintf("sonarr-%d", id),
			Title:   s.title,
			Poster:  fmt.Sprintf("/image/sonarr/%d", id),
			Status:  "downloading",
			Percent: s.percent,
		})
	}
	for _, m := range missing {
		if _, alreadyDownloading := downloading[m.SeriesID]; alreadyDownloading {
			continue
		}
		cards = append(cards, render.DownloadCardView{
			ItemID: fmt.Sprintf("sonarr-%d", m.SeriesID),
			Title:  m.Title,
			Poster: fmt.Sprintf("/image/sonarr/%d", m.SeriesID),
			Status: "searching",
		})
	}
	return cards, nil
}

func percentComplete(size, sizeLeft int64) int {
	if size <= 0 {
		return 0
	}
	pct := int(float64(size-sizeLeft) / float64(size) * 100)
	switch {
	case pct < 0:
		return 0
	case pct > 100:
		return 100
	default:
		return pct
	}
}

// sortDownloadCards orders downloading items first (highest percent first,
// so the closest-to-done card leads), then searching items alphabetically.
func sortDownloadCards(cards []render.DownloadCardView) {
	sort.Slice(cards, func(i, j int) bool {
		a, b := cards[i], cards[j]
		if a.Status != b.Status {
			return a.Status == "downloading"
		}
		if a.Status == "downloading" {
			return a.Percent > b.Percent
		}
		return a.Title < b.Title
	})
}
```

Note: `map` iteration order is non-deterministic in Go, which is why `sortDownloadCards` always runs in `Snapshot` — without it, `TestPoller_MergesRadarrAndSonarr`'s ordering assertion would flake.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test . -run TestPoller`
Expected: PASS, all 7 tests.

- [ ] **Step 5: Commit**

```bash
git add poller.go poller_test.go
git commit -m "Add background poller merging Radarr/Sonarr into a Downloading snapshot"
```

---

### Task 7: Wire it into main.go

**Files:**
- Modify: `main.go`
- Modify: `main_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–6.
- Produces: updated `app`, `newApp`, `widgetHandler`, `imageHandler`, new `liveHandler`, updated `newMux`, updated `main()`.

- [ ] **Step 1: Write the failing tests**

Update `main_test.go`'s `testConfig` helper to include valid Radarr/Sonarr config (required now per Task 4):

```go
func testConfig(jellyfinURL string) *Config {
	return &Config{
		Jellyfin: JellyfinConfig{
			URL:       jellyfinURL,
			Token:     "test-token",
			UserID:    "test-user",
			PublicURL: "https://jellyfin.example.com",
		},
		// 127.0.0.1:1 is a reserved port nothing listens on: "connection
		// refused" comes back near-instantly with no DNS lookup involved,
		// unlike a made-up hostname (which could stall on a slow negative
		// DNS lookup depending on the test environment's resolver).
		Radarr:           RadarrConfig{URL: "http://127.0.0.1:1", Token: "radarr-key"},
		Sonarr:           SonarrConfig{URL: "http://127.0.0.1:1", Token: "sonarr-key"},
		Title:            "Library",
		Limit:            12,
		DownloadingLimit: 12,
	}
}
```

Update the existing image-related assertions/paths from the old `/image/{id}` scheme to the new `/image/jellyfin/{id}` scheme (Task 5's `CardView.ImageSrc` now includes the `jellyfin/` source segment — see Step 3 below):

- In `TestWidgetHandler_EndToEnd`: change `src="/image/abc123"` → `src="/image/jellyfin/abc123"`.
- In `TestWidgetHandler_ImageSrcPrefixedByPublicURL`: change `src="/jellyfin-widget/image/abc123"` → `src="/jellyfin-widget/image/jellyfin/abc123"`.
- In `TestImageHandler_ReachableAtPublicURLPrefix`: change the request target `/jellyfin-widget/image/abc123` → `/jellyfin-widget/image/jellyfin/abc123`.
- In `TestImageHandler_ProxiesImageBytes`: change the request target `/image/abc123` → `/image/jellyfin/abc123`.
- In `TestImageHandler_MissingImageReturns404`: change the request target `/image/does-not-exist` → `/image/jellyfin/does-not-exist`.
- In `TestImageHandler_RejectsPathTraversalItemID`: change the request target `/image/%2e%2e%2Fsecret` → `/image/jellyfin/%2e%2e%2Fsecret`, and update the `req.URL.Path` assertion to `/image/jellyfin/../secret`.

Append new tests to `main_test.go`:

```go
func fakeRadarrServerForApp(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/queue":
			fmt.Fprint(w, `{"records":[{"movieId":1,"size":1000,"sizeleft":500,"movie":{"title":"Downloading Movie"}}]}`)
		case "/api/v3/wanted/missing":
			fmt.Fprint(w, `{"records":[]}`)
		case "/api/v3/MediaCover/1/poster.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("fake-radarr-poster"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func fakeSonarrServerForApp(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/queue":
			fmt.Fprint(w, `{"records":[]}`)
		case "/api/v3/wanted/missing":
			fmt.Fprint(w, `{"records":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func testConfigWithServarr(t *testing.T, jellyfinURL string) *Config {
	t.Helper()
	radarrSrv := fakeRadarrServerForApp(t)
	t.Cleanup(radarrSrv.Close)
	sonarrSrv := fakeSonarrServerForApp(t)
	t.Cleanup(sonarrSrv.Close)

	cfg := testConfig(jellyfinURL)
	cfg.Radarr.URL = radarrSrv.URL
	cfg.Sonarr.URL = sonarrSrv.URL
	return cfg
}

func TestWidgetHandler_IncludesPlayHrefUsingServerID(t *testing.T) {
	jf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/System/Info/Public":
			fmt.Fprint(w, `{"Id":"srv1"}`)
		case r.URL.Path == "/Users/test-user/Items/Latest":
			fmt.Fprint(w, `[{"Id":"abc123","Name":"The Sheep Detectives","Type":"Series","ImageTags":{"Primary":"tag1"}}]`)
		case r.URL.Path == "/Items/abc123/Images/Primary":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("fake-jpeg-bytes"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer jf.Close()

	cfg := testConfigWithServarr(t, jf.URL)
	a := newApp(cfg)
	mux := newMux(cfg, a)

	req := httptest.NewRequest(http.MethodGet, "/widget", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `href="https://jellyfin.example.com/web/#/video?id=abc123&amp;serverId=srv1"`) {
		t.Errorf("body missing play href with server id: %s", body)
	}
}

func TestWidgetHandler_IncludesDownloadingCards(t *testing.T) {
	jf := fakeJellyfinServer(t)
	defer jf.Close()

	cfg := testConfigWithServarr(t, jf.URL)
	a := newApp(cfg)
	a.poller.poll(context.Background())
	mux := newMux(cfg, a)

	req := httptest.NewRequest(http.MethodGet, "/widget", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Downloading Movie") {
		t.Errorf("body missing downloading card: %s", body)
	}
	if !strings.Contains(body, `src="/image/radarr/1"`) {
		t.Errorf("body missing radarr poster src: %s", body)
	}
}

func TestLiveHandler_ServesPollerSnapshot(t *testing.T) {
	jf := fakeJellyfinServer(t)
	defer jf.Close()

	cfg := testConfigWithServarr(t, jf.URL)
	a := newApp(cfg)
	a.poller.poll(context.Background())
	mux := newMux(cfg, a)

	req := httptest.NewRequest(http.MethodGet, "/live.json", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), `"radarr-1"`) {
		t.Errorf("body missing radarr-1 item: %s", rec.Body.String())
	}
}

func TestLiveHandler_ReachableAtPublicURLPrefix(t *testing.T) {
	jf := fakeJellyfinServer(t)
	defer jf.Close()

	cfg := testConfigWithServarr(t, jf.URL)
	cfg.PublicURL = "/jellyfin-widget"
	a := newApp(cfg)
	mux := newMux(cfg, a)

	req := httptest.NewRequest(http.MethodGet, "/jellyfin-widget/live.json", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestImageHandler_RoutesRadarrAndSonarrByPrefix(t *testing.T) {
	jf := fakeJellyfinServer(t)
	defer jf.Close()

	cfg := testConfigWithServarr(t, jf.URL)
	a := newApp(cfg)
	mux := newMux(cfg, a)

	req := httptest.NewRequest(http.MethodGet, "/image/radarr/1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "fake-radarr-poster" {
		t.Errorf("body = %q, want fake-radarr-poster", rec.Body.String())
	}
}

func TestImageHandler_UnknownSourceReturns404(t *testing.T) {
	jf := fakeJellyfinServer(t)
	defer jf.Close()

	cfg := testConfigWithServarr(t, jf.URL)
	a := newApp(cfg)
	mux := newMux(cfg, a)

	req := httptest.NewRequest(http.MethodGet, "/image/unknown/1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
```

Add `"context"` to `main_test.go`'s import block (used by `TestWidgetHandler_IncludesDownloadingCards`/`TestLiveHandler_ServesPollerSnapshot`'s direct `a.poller.poll(...)` call).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test .`
Expected: FAIL — compile errors (`a.poller` undefined, `RadarrConfig`/`SonarrConfig` fields on `testConfig` fine from Task 4 but `app` has no `poller` field yet) and the updated `/image/jellyfin/...` assertions failing against the old handler.

- [ ] **Step 3: Write the implementation**

Replace `main.go` in full:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sidun-av/glance-jellyfin/internal/jellyfin"
	"github.com/sidun-av/glance-jellyfin/internal/radarr"
	"github.com/sidun-av/glance-jellyfin/internal/render"
	"github.com/sidun-av/glance-jellyfin/internal/sonarr"
)

// validItemID matches Jellyfin/Radarr/Sonarr item IDs: Jellyfin's are
// hex-GUID-shaped, Radarr/Sonarr's are plain integers (a subset of hex
// digits), so one allow-list covers all three. Anything else is rejected
// before it can reach an outbound request URL, which splices it in
// unescaped.
var validItemID = regexp.MustCompile(`^[0-9a-fA-F-]+$`)

const downloadPollInterval = 10 * time.Second
const liveClientPollMS = 12000

type app struct {
	cfg            *Config
	jellyfinClient *jellyfin.Client
	radarrClient   *radarr.Client
	sonarrClient   *sonarr.Client
	poller         *downloadPoller

	serverIDMu sync.Mutex
	serverID   string
}

func newApp(cfg *Config) *app {
	jellyfinClient := jellyfin.New(cfg.Jellyfin.URL, cfg.Jellyfin.Token, cfg.Jellyfin.UserID)
	radarrClient := radarr.New(cfg.Radarr.URL, cfg.Radarr.Token)
	sonarrClient := sonarr.New(cfg.Sonarr.URL, cfg.Sonarr.Token)
	poller := newDownloadPoller(radarrClient, sonarrClient, cfg.DownloadingLimit)
	poller.Start(context.Background(), downloadPollInterval)

	return &app{
		cfg:            cfg,
		jellyfinClient: jellyfinClient,
		radarrClient:   radarrClient,
		sonarrClient:   sonarrClient,
		poller:         poller,
	}
}

func liveURL(publicURL string) string {
	return strings.TrimRight(publicURL, "/") + "/live.json"
}

// fetchServerIDCached fetches Jellyfin's server ID (needed for the Play
// deep link) at most once per process: on failure it returns "" and leaves
// nothing cached, so the next request retries rather than being stuck
// without a Play link until a restart.
func (a *app) fetchServerIDCached(ctx context.Context) string {
	a.serverIDMu.Lock()
	defer a.serverIDMu.Unlock()
	if a.serverID != "" {
		return a.serverID
	}
	id, err := a.jellyfinClient.FetchServerID(ctx)
	if err != nil {
		log.Printf("fetch jellyfin server id: %v", err)
		return ""
	}
	a.serverID = id
	return a.serverID
}

func (a *app) widgetHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	w.Header().Set("Widget-Title", a.cfg.Title)
	w.Header().Set("Widget-Content-Type", "html")

	items, err := a.jellyfinClient.FetchLatest(ctx, a.cfg.Limit)
	if err != nil {
		log.Printf("jellyfin unavailable: %v", err)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, render.RenderUnavailable())
		return
	}

	jellyfinPublicURL := strings.TrimRight(a.cfg.Jellyfin.PublicURL, "/")
	imagePrefix := strings.TrimRight(a.cfg.PublicURL, "/")
	serverID := a.fetchServerIDCached(ctx)

	var cards []render.CardView
	for _, it := range items {
		if !it.HasImage {
			continue
		}
		href := jellyfinPublicURL + "/web/#/details?id=" + it.ID
		playHref := href
		if serverID != "" {
			playHref = jellyfinPublicURL + "/web/#/video?id=" + it.ID + "&serverId=" + serverID
		}
		cards = append(cards, render.CardView{
			Title:    it.Name,
			ImageSrc: imagePrefix + "/image/jellyfin/" + it.ID,
			Href:     href,
			PlayHref: playHref,
		})
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, render.RenderWidget(render.WidgetData{
		Cards:          cards,
		Downloading:    a.poller.Snapshot(),
		LiveURL:        liveURL(a.cfg.PublicURL),
		PollIntervalMS: liveClientPollMS,
	}))
}

func (a *app) liveHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	body, err := render.RenderDownloadingLive(a.poller.Snapshot())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

func (a *app) imageHandler(w http.ResponseWriter, r *http.Request) {
	// This handler is reachable at both "/image/{source}/{id}" and
	// "{public_url}/image/{source}/{id}" (see newMux) — strip whichever
	// prefix is actually present, since a reverse proxy may or may not have
	// stripped public_url before forwarding.
	path := r.URL.Path
	if prefix := strings.TrimRight(a.cfg.PublicURL, "/"); prefix != "" && strings.HasPrefix(path, prefix) {
		path = strings.TrimPrefix(path, prefix)
	}
	rest := strings.TrimPrefix(path, "/image/")
	source, itemID, found := strings.Cut(rest, "/")
	if !found || !validItemID.MatchString(itemID) {
		// Empty/malformed itemIDs, path-traversal-shaped itemIDs, and a
		// missing source segment are all rejected here with the same 404 as
		// a genuinely missing image, so this check isn't an oracle for
		// probing valid vs. invalid IDs.
		w.WriteHeader(http.StatusNotFound)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var (
		body        io.ReadCloser
		contentType string
		statusCode  int
	)
	switch source {
	case "jellyfin":
		result, err := a.jellyfinClient.FetchImage(ctx, itemID)
		if err != nil {
			log.Printf("fetch jellyfin image %s: %v", itemID, err)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, contentType, statusCode = result.Body, result.ContentType, result.StatusCode
	case "radarr":
		result, err := a.radarrClient.FetchPoster(ctx, itemID)
		if err != nil {
			log.Printf("fetch radarr poster %s: %v", itemID, err)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, contentType, statusCode = result.Body, result.ContentType, result.StatusCode
	case "sonarr":
		result, err := a.sonarrClient.FetchPoster(ctx, itemID)
		if err != nil {
			log.Printf("fetch sonarr poster %s: %v", itemID, err)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, contentType, statusCode = result.Body, result.ContentType, result.StatusCode
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}
	defer body.Close()

	if statusCode != http.StatusOK {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, body)
}

func newMux(cfg *Config, a *app) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("/widget", a.widgetHandler)
	mux.HandleFunc("/live.json", a.liveHandler)
	mux.HandleFunc("/image/", a.imageHandler)

	// A reverse proxy in front of this service may forward a Custom
	// Location's full original path instead of stripping the public_url
	// prefix (depends on proxy configuration details not every proxy UI
	// makes easy to get right — see glance-homeassistant's README/history
	// for the concrete failure mode this defends against). Registering the
	// image and live-json handlers under that prefix too means they work
	// either way. Only applies when public_url is itself a path — a full
	// origin is a distinct listener reached directly, with no such prefix
	// ever attached.
	if prefix := strings.TrimRight(cfg.PublicURL, "/"); strings.HasPrefix(prefix, "/") {
		mux.HandleFunc(prefix+"/image/", a.imageHandler)
		mux.HandleFunc(prefix+"/live.json", a.liveHandler)
	}
	return mux
}

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/config.yml"
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	a := newApp(cfg)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, newMux(cfg, a)))
}
```

Note: `newApp` now starts the poller's background goroutine immediately (via `context.Background()`, matching this codebase's existing no-graceful-shutdown style), which means every test calling `newApp` spins up a real poller hitting `cfg.Radarr.URL`/`cfg.Sonarr.URL` on a 10s ticker. In tests that don't care about Downloading data, `testConfig`'s `http://127.0.0.1:1` URLs fail near-instantly with "connection refused" and get logged — harmless, but confirm `go test .` output stays otherwise clean (no test failure, just expected log lines) and stays fast before moving on.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./...`
Expected: PASS, every package.

Run: `gofmt -l .`
Expected: no output (all files formatted).

Run: `go vet ./...`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add main.go main_test.go
git commit -m "Wire Play links, Downloading section, and live updates into the widget"
```

---

### Task 8: Deployment docs

**Files:**
- Modify: `config.example.yml`
- Modify: `docker-compose.example.yml`
- Modify: `README.md`

**Interfaces:** none (documentation only).

- [ ] **Step 1: Update `config.example.yml`**

Add after the existing `jellyfin:` block:

```yaml
radarr:
  url: http://radarr:7878   # env: RADARR_URL
  token: replace-with-a-radarr-api-key   # env: RADARR_TOKEN — Settings -> General -> Security

sonarr:
  url: http://sonarr:8989   # env: SONARR_URL
  token: replace-with-a-sonarr-api-key   # env: SONARR_TOKEN — Settings -> General -> Security
```

Add after the existing `limit:` line:

```yaml
downloading_limit: 12   # env: DOWNLOADING_LIMIT — max cards shown in the Downloading section
```

- [ ] **Step 2: Update `docker-compose.example.yml`**

Add to the `environment:` block, after the `JELLYFIN_PUBLIC_URL` line:

```yaml
      - RADARR_URL=${RADARR_URL}
      - RADARR_TOKEN=${RADARR_TOKEN}
      - SONARR_URL=${SONARR_URL}
      - SONARR_TOKEN=${SONARR_TOKEN}
```

Add to the "Optional" block of env vars, alongside `LIMIT`:

```yaml
      - DOWNLOADING_LIMIT=${DOWNLOADING_LIMIT:-}
```

- [ ] **Step 3: Update `README.md`**

In "How it works", after the existing paragraph, add:

```markdown
Two more pieces feed the widget: each Library card also gets a **Play**
button that deep-links straight into Jellyfin's web player (not just its
details page), and a **Downloading** section shows monitored-but-missing
movies/shows sourced from Radarr and Sonarr — a "Searching…" label for
items nothing's been grabbed for yet, and a live-updating progress bar
(polled from `/live.json` every 12s in the browser) for items actively
downloading. Radarr's and Sonarr's own `/api/v3/queue` already reports
download-client progress, so this widget never talks to qBittorrent or
Prowlarr directly.
```

Renumber "Find your Jellyfin user ID" as step 2 (unchanged), and insert a new step 3 before the existing "Expose this service to your browser" step (renumbering it and all subsequent steps up by one):

```markdown
### 3. Create a Radarr API key and a Sonarr API key

In Radarr: **Settings → General → Security** — copy the API Key. Do the same
in Sonarr. These stay server-side (this container's poller and image proxy
use them; the browser never does), so unlike the Jellyfin token above they
need no "public" browser-facing counterpart.
```

In the (renumbered) "Configure" step's bullet list, add:

```markdown
- `RADARR_URL` / `RADARR_TOKEN` — Radarr's base URL (reachable from this
  container, e.g. `http://radarr:7878`) and the API key from step 3.
- `SONARR_URL` / `SONARR_TOKEN` — same, for Sonarr (e.g. `http://sonarr:8989`).
```

In the (renumbered) "Add the widget to Glance" step, change the recommended
`cache:` value and explain why:

```markdown
```yaml
- type: extension
  url: http://glance-jellyfin:8080/widget
  cache: 3m
  allow-potentially-dangerous-html: true
```

`cache: 3m` (down from the Library-only version's `30m`) because the
Downloading section's card set — a download starting or finishing — should
show up reasonably promptly. Progress *numbers* update independently via
`/live.json` every 12 seconds in the browser, so they stay live between
these 3-minute refreshes.
```

In the "Environment variable reference" table, add four rows after
`JELLYFIN_PUBLIC_URL` and one row after `LIMIT`:

```markdown
| `RADARR_URL` | `radarr.url` | — (required) | Radarr base URL, reachable from this container |
| `RADARR_TOKEN` | `radarr.token` | — (required) | Radarr API key |
| `SONARR_URL` | `sonarr.url` | — (required) | Sonarr base URL, reachable from this container |
| `SONARR_TOKEN` | `sonarr.token` | — (required) | Sonarr API key |
```

```markdown
| `DOWNLOADING_LIMIT` | `downloading_limit` | `12` | Max cards shown in the Downloading section |
```

In "Error handling", add:

```markdown
If Radarr or Sonarr is unreachable, the Downloading section simply omits
that source's cards (the rest of the widget, including the other source's
cards, is unaffected) — consistent with the rest of this widget's
philosophy of degrading quietly rather than surfacing a broken state.
```

In "Out of scope", remove "live/real-time updates" from the list (no longer
true — the Downloading section now has them) and add:

```markdown
Per-episode granularity for TV shows (a series with any missing/downloading
episode shows as one card), and talking to qBittorrent or Prowlarr directly
(Radarr/Sonarr's own queue already reports download-client progress) — see
the design spec for the reasoning.
```

- [ ] **Step 4: Verify locally**

Run: `go build ./...` (docs changes don't affect this, but confirms nothing
from Task 7 was left broken) and `go test ./...`.
Expected: both clean.

- [ ] **Step 5: Commit**

```bash
git add config.example.yml docker-compose.example.yml README.md
git commit -m "Document Radarr/Sonarr setup, Play button, and Downloading section"
```
