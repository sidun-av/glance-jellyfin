# Download Statuses + Seerr Search Card Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Downloading section's binary searching/downloading status with a 5-state model (Searching, Downloading, Importing, Stalled, Failed) with an animated indicator for the open-ended states and distinct colors for the two problem states, and add a persistent "Search movies" card linking to Seerr in the Library grid, per `docs/superpowers/specs/2026-07-16-download-statuses-and-seerr-card-design.md`.

**Architecture:** `internal/radarr`/`internal/sonarr`'s `QueueItem` gain two raw fields (`TrackedStatus`, `TrackedState`) already present in Radarr/Sonarr's queue API. `poller.go` gains a pure `classifyStatus` function mapping those two raw fields to one of 4 queue-derived statuses, used by `fetchRadarrCards`/`fetchSonarrCards` in place of today's hardcoded `"downloading"`; Sonarr's per-series aggregation picks by status severity first, percent second. `internal/render` gets per-status markup/CSS (bar only for downloading; a CSS-only animated-dots span for searching/importing; colored static text for stalled/failed) and the live-update bootstrap script learns the status vocabulary. Separately, `Config` gains an optional `Seerr.PublicURL`; `RenderWidget` appends one static "Search movies" card to the Library grid when it's set, wired through `widgetHandler`.

**Tech Stack:** Go 1.23, `net/http` stdlib only, CSS-only animation (no client-side dependency changes).

## Global Constraints

- Radarr/Sonarr's exact `trackedDownloadStatus`/`trackedDownloadState` string values are a best-known-good starting point (`"ok"`/`"warning"`/`"error"` and `"downloading"`/`"importPending"`/`"importing"` respectively) — confirmed/adjusted against the live instance during deployment, the same pattern already established for every other Radarr/Sonarr field this widget reads.
- `Seerr.PublicURL` MUST stay optional with no required-field validation error — the previous feature's final whole-branch review flagged Radarr/Sonarr's hard-required config as tension with this widget's degrade-quietly philosophy; this feature must not repeat that pattern.
- All new color CSS uses `var(--token, fallback-hex)` so it renders correctly regardless of whether Glance's active theme defines that exact token name.
- The animated-dots effect only ever animates the ordinary `width` CSS property via `steps()` keyframes — never the `content` property (inconsistent cross-browser animation support) and never JavaScript-driven (`setInterval` text mutation would fight the live-update poll cycle).
- The live-update bootstrap script (`onerror`-image-tag trick, unchanged mechanism) must never overwrite the animated-dots span's markup — it only ever sets `data-status` and a separate label span's text, so CSS attribute selectors alone control the dots' visibility/animation across live updates.
- Every change: TDD RED→GREEN→REFACTOR, `gofmt -l .` / `go vet ./...` / `go test ./...` clean repo-wide before every commit.

---

### Task 1: Radarr/Sonarr clients — read tracked-download fields

**Files:**
- Modify: `internal/radarr/client.go`
- Modify: `internal/radarr/client_test.go`
- Modify: `internal/sonarr/client.go`
- Modify: `internal/sonarr/client_test.go`

**Interfaces:**
- Produces: `radarr.QueueItem.TrackedStatus string`, `radarr.QueueItem.TrackedState string` (and the same two fields on `sonarr.QueueItem`) — new fields on existing types, additive only. Consumed by Task 2's `classifyStatus`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/radarr/client_test.go`:

```go
func TestFetchQueue_ParsesTrackedDownloadFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"records":[
			{"movieId":1,"size":1000,"sizeleft":500,"trackedDownloadStatus":"warning","trackedDownloadState":"downloading","movie":{"title":"Stalled Movie"}}
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
	if items[0].TrackedStatus != "warning" {
		t.Errorf("TrackedStatus = %q, want warning", items[0].TrackedStatus)
	}
	if items[0].TrackedState != "downloading" {
		t.Errorf("TrackedState = %q, want downloading", items[0].TrackedState)
	}
}

func TestFetchQueue_MissingTrackedFieldsDefaultToEmptyNotError(t *testing.T) {
	// A record with no trackedDownloadStatus/trackedDownloadState at all
	// (e.g. an older Radarr version's shape) must still parse cleanly —
	// Go's JSON decoder leaves missing string fields as "", not an error.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"records":[{"movieId":1,"size":1000,"sizeleft":500,"movie":{"title":"M"}}]}`))
	}))
	defer server.Close()

	client := New(server.URL, "test-key")
	items, err := client.FetchQueue(context.Background())
	if err != nil {
		t.Fatalf("FetchQueue: %v", err)
	}
	if items[0].TrackedStatus != "" || items[0].TrackedState != "" {
		t.Errorf("items[0] = %+v, want empty TrackedStatus/TrackedState", items[0])
	}
}
```

Append to `internal/sonarr/client_test.go`:

```go
func TestFetchQueue_ParsesTrackedDownloadFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"records":[
			{"seriesId":5,"size":1000,"sizeleft":500,"trackedDownloadStatus":"error","trackedDownloadState":"failedPending","series":{"title":"Failed Show"}}
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
	if items[0].TrackedStatus != "error" {
		t.Errorf("TrackedStatus = %q, want error", items[0].TrackedStatus)
	}
	if items[0].TrackedState != "failedPending" {
		t.Errorf("TrackedState = %q, want failedPending", items[0].TrackedState)
	}
}

func TestFetchQueue_MissingTrackedFieldsDefaultToEmptyNotError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"records":[{"seriesId":5,"size":1000,"sizeleft":500,"series":{"title":"S"}}]}`))
	}))
	defer server.Close()

	client := New(server.URL, "test-key")
	items, err := client.FetchQueue(context.Background())
	if err != nil {
		t.Fatalf("FetchQueue: %v", err)
	}
	if items[0].TrackedStatus != "" || items[0].TrackedState != "" {
		t.Errorf("items[0] = %+v, want empty TrackedStatus/TrackedState", items[0])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/radarr/... ./internal/sonarr/...`
Expected: FAIL — `TrackedStatus`/`TrackedState` undefined on `QueueItem`.

- [ ] **Step 3: Write the implementation**

In `internal/radarr/client.go`, update `QueueItem` and `FetchQueue`:

```go
type QueueItem struct {
	MovieID       int
	Title         string
	Size          int64
	SizeLeft      int64
	TrackedStatus string // raw trackedDownloadStatus: "ok" | "warning" | "error"
	TrackedState  string // raw trackedDownloadState: "downloading" | "importPending" | "importing" | ...
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
			MovieID               int    `json:"movieId"`
			Size                  int64  `json:"size"`
			SizeLeft              int64  `json:"sizeleft"`
			TrackedDownloadStatus string `json:"trackedDownloadStatus"`
			TrackedDownloadState  string `json:"trackedDownloadState"`
			Movie                 struct {
				Title string `json:"title"`
			} `json:"movie"`
		} `json:"records"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse queue response: %w", err)
	}

	items := make([]QueueItem, len(raw.Records))
	for i, r := range raw.Records {
		items[i] = QueueItem{
			MovieID:       r.MovieID,
			Title:         r.Movie.Title,
			Size:          r.Size,
			SizeLeft:      r.SizeLeft,
			TrackedStatus: r.TrackedDownloadStatus,
			TrackedState:  r.TrackedDownloadState,
		}
	}
	return items, nil
}
```

In `internal/sonarr/client.go`, the identical change with `SeriesID`:

```go
type QueueItem struct {
	SeriesID      int
	Title         string
	Size          int64
	SizeLeft      int64
	TrackedStatus string // raw trackedDownloadStatus: "ok" | "warning" | "error"
	TrackedState  string // raw trackedDownloadState: "downloading" | "importPending" | "importing" | ...
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
			SeriesID              int    `json:"seriesId"`
			Size                  int64  `json:"size"`
			SizeLeft              int64  `json:"sizeleft"`
			TrackedDownloadStatus string `json:"trackedDownloadStatus"`
			TrackedDownloadState  string `json:"trackedDownloadState"`
			Series                struct {
				Title string `json:"title"`
			} `json:"series"`
		} `json:"records"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse queue response: %w", err)
	}

	items := make([]QueueItem, len(raw.Records))
	for i, r := range raw.Records {
		items[i] = QueueItem{
			SeriesID:      r.SeriesID,
			Title:         r.Series.Title,
			Size:          r.Size,
			SizeLeft:      r.SizeLeft,
			TrackedStatus: r.TrackedDownloadStatus,
			TrackedState:  r.TrackedDownloadState,
		}
	}
	return items, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/radarr/... ./internal/sonarr/...`
Expected: PASS, all tests including the 4 new ones.

- [ ] **Step 5: Commit**

```bash
git add internal/radarr/client.go internal/radarr/client_test.go internal/sonarr/client.go internal/sonarr/client_test.go
git commit -m "Read trackedDownloadStatus/trackedDownloadState from Radarr/Sonarr queue records"
```

---

### Task 2: Poller — classify queue items into 5 statuses

**Files:**
- Modify: `poller.go`
- Modify: `poller_test.go`

**Interfaces:**
- Consumes: `radarr.QueueItem.TrackedStatus`/`TrackedState`, `sonarr.QueueItem.TrackedStatus`/`TrackedState` (Task 1).
- Produces: `classifyStatus(trackedStatus, trackedState string) string` returning one of `"downloading"`, `"importing"`, `"stalled"`, `"failed"`. `fetchRadarrCards`/`fetchSonarrCards` now assign `render.DownloadCardView.Status` from this instead of a hardcoded `"downloading"`. Consumed by Task 3 (render markup) — no other task depends on the function itself, only on the wider `Status` values it produces.

- [ ] **Step 1: Write the failing tests**

Append to `poller_test.go`:

```go
func TestClassifyStatus_PriorityOrder(t *testing.T) {
	cases := []struct {
		name          string
		trackedStatus string
		trackedState  string
		want          string
	}{
		{"plain downloading", "ok", "downloading", "downloading"},
		{"empty fields default to downloading", "", "", "downloading"},
		{"importPending", "ok", "importPending", "importing"},
		{"importing", "ok", "importing", "importing"},
		{"warning stalls even mid-import", "warning", "importing", "stalled"},
		{"error wins over warning-shaped state", "error", "importPending", "failed"},
		{"error wins over plain downloading", "error", "downloading", "failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyStatus(c.trackedStatus, c.trackedState)
			if got != c.want {
				t.Errorf("classifyStatus(%q, %q) = %q, want %q", c.trackedStatus, c.trackedState, got, c.want)
			}
		})
	}
}

func TestPoller_RadarrUsesClassifiedStatus(t *testing.T) {
	rr := fakeRadarrServer(
		`{"records":[{"movieId":1,"size":1000,"sizeleft":500,"trackedDownloadStatus":"error","trackedDownloadState":"failed","movie":{"title":"Broken Movie"}}]}`,
		`{"records":[]}`,
	)
	defer rr.Close()
	sr := fakeSonarrServer(`{"records":[]}`, `{"records":[]}`)
	defer sr.Close()

	p := newDownloadPoller(radarr.New(rr.URL, "k"), sonarr.New(sr.URL, "k"), 12)
	p.poll(context.Background())

	got := p.Snapshot()
	if len(got) != 1 || got[0].Status != "failed" {
		t.Fatalf("snapshot = %+v, want one failed card", got)
	}
}

func TestPoller_SonarrAggregatesBySeverityThenPercent(t *testing.T) {
	// Two episodes of the same series: one 90% downloaded with no
	// problems, one 10% downloaded but failed. The series card must
	// surface the failed episode — severity beats percent, since the
	// whole point of this feature is "show what needs attention".
	sr := fakeSonarrServer(
		`{"records":[
			{"seriesId":5,"size":1000,"sizeleft":100,"trackedDownloadStatus":"ok","trackedDownloadState":"downloading","series":{"title":"Some Show"}},
			{"seriesId":5,"size":1000,"sizeleft":900,"trackedDownloadStatus":"error","trackedDownloadState":"failed","series":{"title":"Some Show"}}
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
		t.Fatalf("len(snapshot) = %d, want 1 (one card per series)", len(got))
	}
	if got[0].Status != "failed" {
		t.Errorf("Status = %q, want failed (severity beats percent)", got[0].Status)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test . -run "TestClassifyStatus|TestPoller_RadarrUsesClassifiedStatus|TestPoller_SonarrAggregatesBySeverityThenPercent"`
Expected: FAIL — `classifyStatus` undefined; the two poller tests fail because `Status` is always `"downloading"` today regardless of tracked fields.

- [ ] **Step 3: Write the implementation**

In `poller.go`, add `classifyStatus` (place it near `percentComplete`):

```go
// classifyStatus derives one of "downloading", "importing", "stalled", or
// "failed" from Radarr/Sonarr's raw trackedDownloadStatus/trackedDownloadState
// queue fields. Priority: an error always wins (something needs attention
// right now), a warning wins over anything except an error (e.g. a stalled
// torrent with no seeds), a still-processing-after-download state wins over
// plain downloading, and the default is plain downloading — the common
// case, when nothing above applies.
func classifyStatus(trackedStatus, trackedState string) string {
	switch {
	case trackedStatus == "error":
		return "failed"
	case trackedStatus == "warning":
		return "stalled"
	case trackedState == "importPending" || trackedState == "importing":
		return "importing"
	default:
		return "downloading"
	}
}

// statusSeverity ranks the 4 queue-derived statuses by how much attention
// they need, highest first — used to pick which episode represents a
// Sonarr series when several are queued simultaneously with different
// statuses (see fetchSonarrCards).
var statusSeverity = map[string]int{
	"failed":      3,
	"stalled":     2,
	"importing":   1,
	"downloading": 0,
}
```

Update `fetchRadarrCards`'s queue loop:

```go
	downloading := make(map[int]render.DownloadCardView, len(queue))
	for _, q := range queue {
		downloading[q.MovieID] = render.DownloadCardView{
			ItemID:  fmt.Sprintf("radarr-%d", q.MovieID),
			Title:   q.Title,
			Poster:  fmt.Sprintf("/image/radarr/%d", q.MovieID),
			Status:  classifyStatus(q.TrackedStatus, q.TrackedState),
			Percent: percentComplete(q.Size, q.SizeLeft),
		}
	}
```

Update `fetchSonarrCards`'s aggregation to track and compare status severity, not just percent:

```go
	type seriesState struct {
		title   string
		percent int
		status  string
	}
	downloading := make(map[int]seriesState, len(queue))
	for _, q := range queue {
		status := classifyStatus(q.TrackedStatus, q.TrackedState)
		pct := percentComplete(q.Size, q.SizeLeft)
		existing, ok := downloading[q.SeriesID]
		if !ok ||
			statusSeverity[status] > statusSeverity[existing.status] ||
			(statusSeverity[status] == statusSeverity[existing.status] && pct > existing.percent) {
			downloading[q.SeriesID] = seriesState{title: q.Title, percent: pct, status: status}
		}
	}

	cards := make([]render.DownloadCardView, 0, len(downloading)+len(missing))
	for id, s := range downloading {
		cards = append(cards, render.DownloadCardView{
			ItemID:  fmt.Sprintf("sonarr-%d", id),
			Title:   s.title,
			Poster:  fmt.Sprintf("/image/sonarr/%d", id),
			Status:  s.status,
			Percent: s.percent,
		})
	}
```

(The rest of `fetchSonarrCards` — the missing-episode loop building `"searching"` cards — is unchanged.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test . -run "TestClassifyStatus|TestPoller"`
Expected: PASS, all tests including the 3 new ones. Re-run with `-count=10 -race` to confirm the severity-then-percent comparison introduces no new map-iteration-order flakiness (mirrors this file's established practice from its last review round).

- [ ] **Step 5: Commit**

```bash
git add poller.go poller_test.go
git commit -m "Classify queue items into downloading/importing/stalled/failed statuses"
```

---

### Task 3: Render — 5-status markup, animated dots, live-update labels

**Files:**
- Modify: `internal/render/downloading.go`
- Modify: `internal/render/grid.go`
- Modify: `internal/render/grid_test.go`

**Interfaces:**
- Consumes: `DownloadCardView.Status` now carrying one of 5 values (Task 2 makes the poller actually produce 4 of them; `"searching"` is unchanged and pre-existing).
- Produces: no new exported symbols — `renderDownloadingSection` and `bootstrapScript`'s behavior changes internally. Nothing outside `internal/render` depends on this task directly.

- [ ] **Step 1: Write the failing tests**

Append to `internal/render/grid_test.go`:

```go
func TestRenderWidget_RendersImportingStalledFailedStatuses(t *testing.T) {
	html := RenderWidget(WidgetData{Downloading: []DownloadCardView{
		{ItemID: "radarr-1", Title: "Importing Movie", Poster: "/p/1", Status: "importing"},
		{ItemID: "radarr-2", Title: "Stalled Movie", Poster: "/p/2", Status: "stalled"},
		{ItemID: "radarr-3", Title: "Failed Movie", Poster: "/p/3", Status: "failed"},
	}})
	if !contains(html, `data-status="importing"`) || !contains(html, "Importing") {
		t.Errorf("html missing importing card markup: %q", html)
	}
	if !contains(html, `data-status="stalled"`) || !contains(html, "Stalled") {
		t.Errorf("html missing stalled card markup: %q", html)
	}
	if !contains(html, `data-status="failed"`) || !contains(html, "Failed") {
		t.Errorf("html missing failed card markup: %q", html)
	}
}

func TestRenderWidget_ProgressBarOnlyShownForDownloading(t *testing.T) {
	html := RenderWidget(WidgetData{Downloading: []DownloadCardView{
		{ItemID: "radarr-1", Title: "M", Poster: "/p/1", Status: "downloading", Percent: 55},
	}})
	if !contains(html, `.jf-dl-status:not([data-status="downloading"]) .jf-dl-bar{display:none}`) {
		t.Errorf("CSS doesn't hide the bar for every non-downloading status: %q", html)
	}
}

func TestRenderWidget_StalledAndFailedHaveDistinctColors(t *testing.T) {
	html := RenderWidget(WidgetData{Downloading: []DownloadCardView{
		{ItemID: "radarr-1", Title: "M", Poster: "/p/1", Status: "stalled"},
	}})
	if !contains(html, `[data-status="stalled"] .jf-dl-pct{color:var(--color-warning,#e0a458)}`) {
		t.Errorf("stalled status isn't styled with the warning color: %q", html)
	}
	if !contains(html, `[data-status="failed"] .jf-dl-pct{color:var(--color-negative,#e05f5f)}`) {
		t.Errorf("failed status isn't styled with the negative color: %q", html)
	}
}

func TestRenderWidget_AnimatedDotsOnlyForSearchingAndImporting(t *testing.T) {
	html := RenderWidget(WidgetData{Downloading: []DownloadCardView{
		{ItemID: "radarr-1", Title: "M", Poster: "/p/1", Status: "searching"},
	}})
	if !contains(html, `class="jf-dl-dots"`) {
		t.Errorf("html missing the animated-dots span: %q", html)
	}
	if !contains(html, `[data-status="searching"] .jf-dl-dots{display:inline-block;animation:jf-dl-dots`) {
		t.Errorf("dots aren't animated for searching: %q", html)
	}
	if !contains(html, `[data-status="importing"] .jf-dl-dots{display:inline-block;animation:jf-dl-dots`) {
		t.Errorf("dots aren't animated for importing: %q", html)
	}
	if !contains(html, `@keyframes jf-dl-dots`) {
		t.Errorf("html missing the dots keyframes: %q", html)
	}
}

func TestRenderWidget_BootstrapScriptKnowsAllStatusLabels(t *testing.T) {
	html := RenderWidget(WidgetData{LiveURL: "/live.json", PollIntervalMS: 12000})
	for _, want := range []string{"searching:'Searching'", "importing:'Importing'", "stalled:'Stalled'", "failed:'Failed'"} {
		if !contains(html, want) {
			t.Errorf("bootstrap script missing label mapping %q: %q", want, html)
		}
	}
	if contains(html, "Searching…") {
		t.Errorf("bootstrap script still has the old hardcoded ellipsis fallback: %q", html)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/render/...`
Expected: FAIL — none of the new CSS/markup/JS exists yet.

- [ ] **Step 3: Write the implementation**

Replace `internal/render/downloading.go`'s `DownloadCardView` doc comment and `renderDownloadingSection` in full:

```go
package render

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
)

// DownloadCardView.Status is one of "searching", "downloading",
// "importing", "stalled", or "failed".
type DownloadCardView struct {
	ItemID  string
	Title   string
	Poster  string
	Status  string
	Percent int
}

func renderDownloadingSection(items []DownloadCardView) string {
	var b strings.Builder
	b.WriteString(`<style>
	[data-item-id]{display:block}
	.jf-dl-status{margin-top:4px}
	.jf-dl-bar{height:4px;border-radius:2px;background:var(--color-widget-background-highlight);overflow:hidden}
	.jf-dl-status:not([data-status="downloading"]) .jf-dl-bar{display:none}
	.jf-dl-fill{height:100%;background:var(--color-primary)}
	.jf-dl-pct{font-size:10px;color:var(--color-text-subdue);margin-top:2px}
	.jf-dl-status[data-status="searching"] .jf-dl-pct{color:var(--color-text-highlight)}
	.jf-dl-status[data-status="importing"] .jf-dl-pct{color:var(--color-text-highlight)}
	.jf-dl-status[data-status="stalled"] .jf-dl-pct{color:var(--color-warning,#e0a458)}
	.jf-dl-status[data-status="failed"] .jf-dl-pct{color:var(--color-negative,#e05f5f)}
	.jf-dl-dots{display:none;width:0;overflow:hidden;vertical-align:bottom}
	.jf-dl-status[data-status="searching"] .jf-dl-dots{display:inline-block;animation:jf-dl-dots 1.4s steps(4) infinite}
	.jf-dl-status[data-status="importing"] .jf-dl-dots{display:inline-block;animation:jf-dl-dots 1.4s steps(4) infinite}
	@keyframes jf-dl-dots{to{width:1.6em}}
</style>`)
	b.WriteString(`<div class="jf-section-label">Downloading</div><div class="jf-grid">`)
	for _, item := range items {
		label := statusLabel(item)
		width := 0
		if item.Status == "downloading" {
			width = item.Percent
		}
		fmt.Fprintf(&b,
			`<div class="jf-dl-card" data-item-id="%s"><img class="jf-poster" src="%s" alt="%s" loading="lazy"><div class="jf-title">%s</div><div class="jf-dl-status" data-status="%s"><div class="jf-dl-bar"><div class="jf-dl-fill" style="width:%d%%"></div></div><div class="jf-dl-pct"><span class="jf-dl-label">%s</span><span class="jf-dl-dots">....</span></div></div></div>`,
			html.EscapeString(item.ItemID), html.EscapeString(item.Poster), html.EscapeString(item.Title),
			html.EscapeString(item.Title), html.EscapeString(item.Status), width, html.EscapeString(label),
		)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// statusLabel returns the short text shown for a card's current status:
// a live percentage while downloading, otherwise a fixed word per status.
func statusLabel(item DownloadCardView) string {
	switch item.Status {
	case "downloading":
		return fmt.Sprintf("%d%%", item.Percent)
	case "importing":
		return "Importing"
	case "stalled":
		return "Stalled"
	case "failed":
		return "Failed"
	default: // "searching"
		return "Searching"
	}
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

In `internal/render/grid.go`, replace the `bootstrapScript` const:

```go
const bootstrapScript = `(function(img){var root=img.closest('.jf-widget');if(!root)return;var url=root.dataset.liveUrl;var interval=parseInt(root.dataset.pollMs,10)||12000;var labels={searching:'Searching',importing:'Importing',stalled:'Stalled',failed:'Failed'};function applyState(data){(data.items||[]).forEach(function(item){var card=root.querySelector('.jf-dl-card[data-item-id="'+CSS.escape(item.item_id)+'"]');if(!card)return;var status=card.querySelector('.jf-dl-status');if(!status)return;status.dataset.status=item.status;var fill=status.querySelector('.jf-dl-fill');if(fill)fill.style.width=(item.status==='downloading'?item.percent:0)+'%';var label=status.querySelector('.jf-dl-label');if(label)label.textContent=item.status==='downloading'?item.percent+'%':(labels[item.status]||item.status);});}function poll(){fetch(url,{cache:'no-store'}).then(function(r){return r.ok?r.json():null;}).then(function(data){if(data)applyState(data);}).catch(function(){});}setInterval(poll,interval);poll();})(this)`
```

(This replaces the old script's hardcoded `item.status==='downloading'?item.percent+'%':'Searching…'` fallback with the `labels` lookup, and updates `.jf-dl-pct` targeting to `.jf-dl-label` so the sibling `.jf-dl-dots` span is never touched by JS.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/render/...`
Expected: PASS, all tests including the 5 new ones and every pre-existing test (the existing `TestRenderWidget_RendersDownloadingCards` assertions all remain true substrings of the new markup — verify this explicitly rather than assuming).

- [ ] **Step 5: Commit**

```bash
git add internal/render/downloading.go internal/render/grid.go internal/render/grid_test.go
git commit -m "Render 5-status Downloading cards with animated dots and status colors"
```

---

### Task 4: Config — optional Seerr URL

**Files:**
- Modify: `config.go`
- Modify: `config_test.go`

**Interfaces:**
- Produces: `Config.Seerr SeerrConfig`, `SeerrConfig{PublicURL string}`. Consumed by Task 6 (`main.go`).

- [ ] **Step 1: Write the failing tests**

Append to `config_test.go`:

```go
func TestLoadConfig_SeerrOptional(t *testing.T) {
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
		t.Fatalf("LoadConfig: %v (seerr must be optional)", err)
	}
	if cfg.Seerr.PublicURL != "" {
		t.Errorf("Seerr.PublicURL = %q, want empty (no config given)", cfg.Seerr.PublicURL)
	}
}

func TestLoadConfig_SeerrEnvOverride(t *testing.T) {
	setEnv(t, "JELLYFIN_URL", "http://jellyfin:8096")
	setEnv(t, "JELLYFIN_TOKEN", "t")
	setEnv(t, "JELLYFIN_USER_ID", "u")
	setEnv(t, "JELLYFIN_PUBLIC_URL", "https://jf.example.com")
	setEnv(t, "RADARR_URL", "http://radarr:7878")
	setEnv(t, "RADARR_TOKEN", "r")
	setEnv(t, "SONARR_URL", "http://sonarr:8989")
	setEnv(t, "SONARR_TOKEN", "s")
	setEnv(t, "SEERR_PUBLIC_URL", "https://seerr.example.com")

	path := writeTempConfig(t, `title: ignored`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Seerr.PublicURL != "https://seerr.example.com" {
		t.Errorf("Seerr.PublicURL = %q, want env override", cfg.Seerr.PublicURL)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test .`
Expected: FAIL — `cfg.Seerr` undefined (compile error).

- [ ] **Step 3: Write the implementation**

In `config.go`, update `Config` and add `SeerrConfig`:

```go
type Config struct {
	Jellyfin JellyfinConfig `yaml:"jellyfin"`
	Radarr   RadarrConfig   `yaml:"radarr"`
	Sonarr   SonarrConfig   `yaml:"sonarr"`
	Seerr    SeerrConfig    `yaml:"seerr"`
	// PublicURL is THIS service's own path/origin, reachable from the
	// browser — used to prefix the poster <img src="..."> URLs so they
	// reach this container through a reverse proxy, exactly like
	// jellyfin.public_url is Jellyfin's own browser-facing URL for
	// click-through links. Not required: "" means this service is served
	// from the site root.
	PublicURL        string `yaml:"public_url"`
	Title            string `yaml:"title"`
	Limit            int    `yaml:"limit"`
	DownloadingLimit int    `yaml:"downloading_limit"`
}
```

```go
// SeerrConfig.PublicURL is optional — empty means the Library grid's
// "Search movies" card simply doesn't render. Unlike Radarr/Sonarr, this
// widget makes no Seerr API calls at all (it's a static link), so there's
// no token field here.
type SeerrConfig struct {
	PublicURL string `yaml:"public_url"`
}
```

In `applyEnvOverrides`, after the existing `SONARR_TOKEN` block, add:

```go
	if v, ok := lookupNonEmptyEnv("SEERR_PUBLIC_URL"); ok {
		cfg.Seerr.PublicURL = v
	}
```

No required-field check is added for `Seerr.PublicURL` — this is deliberate (see the plan's Global Constraints).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test .`
Expected: PASS, all tests including the 2 new ones.

- [ ] **Step 5: Commit**

```bash
git add config.go config_test.go
git commit -m "Add optional Seerr public URL config"
```

---

### Task 5: Render — Seerr search card

**Files:**
- Modify: `internal/render/grid.go`
- Modify: `internal/render/grid_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `WidgetData.SeerrURL string` (new field). Consumed by Task 6 (`main.go`).

- [ ] **Step 1: Write the failing tests**

`grid_test.go` currently only imports `"testing"` (it uses hand-rolled `contains`/`count` helpers instead of the `strings` package). `TestRenderWidget_SeerrCardIsLastInGrid` below needs `strings.Index`/`strings.LastIndex` for a positional check the existing helpers don't provide — add `"strings"` to the import block:

```go
import (
	"strings"
	"testing"
)
```

Append to `internal/render/grid_test.go`:

```go
func TestRenderWidget_SeerrCardAppearsWhenConfigured(t *testing.T) {
	html := RenderWidget(WidgetData{
		Cards:    []CardView{{Title: "A Movie", ImageSrc: "/image/jellyfin/1", Href: "/x"}},
		SeerrURL: "https://seerr.example.com",
	})
	if !contains(html, `class="jf-seerr-card"`) {
		t.Errorf("html missing seerr card: %q", html)
	}
	if !contains(html, `href="https://seerr.example.com"`) {
		t.Errorf("html missing seerr card href: %q", html)
	}
	if !contains(html, "Search movies") {
		t.Errorf("html missing seerr card caption: %q", html)
	}
}

func TestRenderWidget_SeerrCardAbsentWhenNotConfigured(t *testing.T) {
	html := RenderWidget(WidgetData{
		Cards: []CardView{{Title: "A Movie", ImageSrc: "/image/jellyfin/1", Href: "/x"}},
	})
	if contains(html, "jf-seerr-card") {
		t.Errorf("html has a seerr card when SeerrURL is empty: %q", html)
	}
}

func TestRenderWidget_SeerrCardIsLastInGrid(t *testing.T) {
	html := RenderWidget(WidgetData{
		Cards: []CardView{
			{Title: "First", ImageSrc: "/image/jellyfin/1", Href: "/1"},
			{Title: "Second", ImageSrc: "/image/jellyfin/2", Href: "/2"},
		},
		SeerrURL: "https://seerr.example.com",
	})
	lastCard := strings.LastIndex(html, `class="jf-card"`)
	seerrCard := strings.Index(html, `class="jf-seerr-card"`)
	if seerrCard < lastCard {
		t.Errorf("seerr card is not positioned after the real cards: %q", html)
	}
}

func TestRenderWidget_SeerrCardShowsEvenWhenLibraryEmpty(t *testing.T) {
	html := RenderWidget(WidgetData{Cards: nil, SeerrURL: "https://seerr.example.com"})
	if contains(html, "jf-empty") {
		t.Errorf("html shows the empty-library message even though the seerr card should render: %q", html)
	}
	if !contains(html, `class="jf-seerr-card"`) {
		t.Errorf("html missing seerr card when library is empty: %q", html)
	}
}

func TestRenderWidget_SeerrCardEscapesURL(t *testing.T) {
	html := RenderWidget(WidgetData{SeerrURL: `"><script>alert(1)</script>`})
	if contains(html, `<script>alert(1)</script>`) {
		t.Errorf("seerr URL not escaped: %q", html)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/render/...`
Expected: FAIL — `WidgetData.SeerrURL` undefined (compile error).

- [ ] **Step 3: Write the implementation**

In `internal/render/grid.go`:

Add `SeerrURL` to `WidgetData`:

```go
type WidgetData struct {
	Cards          []CardView
	Downloading    []DownloadCardView
	SeerrURL       string
	LiveURL        string
	PollIntervalMS int
}
```

Add to `styleBlock()`, after the existing `.jf-section-label` rule:

```go
	.jf-seerr-card{display:flex;flex-direction:column;align-items:center;justify-content:center;aspect-ratio:2/3;border:1px dashed var(--color-text-subdue);border-radius:6px;color:inherit;text-decoration:none;gap:6px}
	.jf-seerr-icon{font-size:28px;opacity:.8}
```

Replace `RenderWidget`'s card-grid section:

```go
	if len(data.Cards) == 0 && data.SeerrURL == "" {
		b.WriteString(`<div class="jf-empty">no recently added items found</div>`)
	} else {
		b.WriteString(`<div class="jf-grid">`)
		for _, c := range data.Cards {
			b.WriteString(renderCard(c))
		}
		if data.SeerrURL != "" {
			b.WriteString(renderSeerrCard(data.SeerrURL))
		}
		b.WriteString(`</div>`)
	}
```

Add a new function, near `renderCard`:

```go
func renderSeerrCard(seerrURL string) string {
	return fmt.Sprintf(
		`<a class="jf-seerr-card" href="%s" target="_blank" rel="noopener"><div class="jf-seerr-icon">&#128269;</div><div class="jf-title">Search movies</div></a>`,
		html.EscapeString(seerrURL),
	)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/render/...`
Expected: PASS, all tests including the 5 new ones.

- [ ] **Step 5: Commit**

```bash
git add internal/render/grid.go internal/render/grid_test.go
git commit -m "Add a persistent Search-movies card linking to Seerr"
```

---

### Task 6: Wire Seerr URL into main.go

**Files:**
- Modify: `main.go`
- Modify: `main_test.go`

**Interfaces:**
- Consumes: `Config.Seerr.PublicURL` (Task 4), `render.WidgetData.SeerrURL` (Task 5).
- Produces: no new exported symbols — `widgetHandler` now threads one more field through.

- [ ] **Step 1: Write the failing test**

Append to `main_test.go`:

```go
func TestWidgetHandler_IncludesSeerrCardWhenConfigured(t *testing.T) {
	jf := fakeJellyfinServer(t)
	defer jf.Close()

	cfg := testConfigWithServarr(t, jf.URL)
	cfg.Seerr.PublicURL = "https://seerr.example.com"
	a := newApp(cfg)
	mux := newMux(cfg, a)

	req := httptest.NewRequest(http.MethodGet, "/widget", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `class="jf-seerr-card"`) {
		t.Errorf("body missing seerr card: %s", body)
	}
	if !strings.Contains(body, `href="https://seerr.example.com"`) {
		t.Errorf("body missing seerr card href: %s", body)
	}
}

func TestWidgetHandler_OmitsSeerrCardWhenNotConfigured(t *testing.T) {
	jf := fakeJellyfinServer(t)
	defer jf.Close()

	cfg := testConfigWithServarr(t, jf.URL) // cfg.Seerr.PublicURL left empty
	a := newApp(cfg)
	mux := newMux(cfg, a)

	req := httptest.NewRequest(http.MethodGet, "/widget", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "jf-seerr-card") {
		t.Errorf("body has a seerr card when Seerr.PublicURL is unset: %s", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test . -run TestWidgetHandler_.*Seerr`
Expected: FAIL — `render.WidgetData` has no `SeerrURL` set from `widgetHandler`, so the first test's assertions fail (the second already passes trivially, but is written first per TDD to document the default).

- [ ] **Step 3: Write the implementation**

In `main.go`'s `widgetHandler`, add `SeerrURL` to the `render.WidgetData` literal:

```go
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, render.RenderWidget(render.WidgetData{
		Cards:          cards,
		Downloading:    downloadingCards,
		SeerrURL:       strings.TrimRight(a.cfg.Seerr.PublicURL, "/"),
		LiveURL:        liveURL(a.cfg.PublicURL),
		PollIntervalMS: liveClientPollMS,
	}))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./...`
Expected: PASS, every package.

Run: `gofmt -l .` and `go vet ./...`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add main.go main_test.go
git commit -m "Wire Seerr URL into the widget's Search-movies card"
```

---

### Task 7: Deployment docs

**Files:**
- Modify: `config.example.yml`
- Modify: `docker-compose.example.yml`
- Modify: `README.md`

**Interfaces:** none (documentation only).

- [ ] **Step 1: Update `config.example.yml`**

Add after the existing `sonarr:` block, before `public_url:`:

```yaml
seerr:
  public_url: https://seerr.example.com   # env: SEERR_PUBLIC_URL — optional; omit entirely to leave the "Search movies" card off the widget
```

- [ ] **Step 2: Update `docker-compose.example.yml`**

Add to the "Optional" block of env vars, alongside `LIMIT`/`DOWNLOADING_LIMIT`:

```yaml
      - SEERR_PUBLIC_URL=${SEERR_PUBLIC_URL:-}
```

- [ ] **Step 3: Update `README.md`**

In "How it works", after the existing "Two more pieces feed the widget" paragraph, add:

```markdown
The Downloading section's cards also carry more than just "searching" or
"downloading": Radarr/Sonarr's queue already reports whether a download is
still processing after finishing ("Importing…"), stuck with no progress
("Stalled", shown in amber), or has outright failed ("Failed", shown in
red) — this widget surfaces all of it instead of just the two most common
states. A separate, always-present card at the end of the Library grid
links straight to Seerr's search page, if you've set `SEERR_PUBLIC_URL`.
```

In the numbered Setup steps, add a new step 8 after the existing step 7 ("Add the widget to Glance"):

```markdown
### 8. (Optional) Link to Seerr

If you run [Seerr](https://github.com/Fallenbagel/jellyseerr) (or Overseerr)
for content requests, set `SEERR_PUBLIC_URL` to its browser-facing URL and a
"Search movies" card appears at the end of the Library grid, linking there.
This is a static link only — this widget makes no Seerr API calls, so
there's no token to configure.
```

In the "Environment variable reference" table, add one row after `DOWNLOADING_LIMIT`:

```markdown
| `SEERR_PUBLIC_URL` | `seerr.public_url` | `""` (card omitted) | Seerr's browser-facing URL for the "Search movies" card — optional |
```

In "Error handling", add:

```markdown
If a Radarr/Sonarr download hits a problem, the Downloading section shows
that specific card as "Stalled" or "Failed" in a distinct color rather than
silently leaving it looking like every other download — that's the point of
the richer status model, not something to work around.
```

- [ ] **Step 4: Verify locally**

Run: `go build ./...` and `go test ./...` (docs changes shouldn't affect either, but confirms nothing was left broken).
Expected: both clean.

- [ ] **Step 5: Commit**

```bash
git add config.example.yml docker-compose.example.yml README.md
git commit -m "Document richer download statuses and the optional Seerr search card"
```
