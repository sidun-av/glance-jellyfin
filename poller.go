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
			Status:  classifyStatus(q.TrackedStatus, q.TrackedState),
			Percent: percentComplete(q.Size, q.SizeLeft),
		}
	}

	cards := make([]render.DownloadCardView, 0, len(downloading)+len(missing))
	for _, d := range downloading {
		cards = append(cards, d)
	}
	searching := make(map[int]bool, len(missing))
	for _, m := range missing {
		if _, alreadyDownloading := downloading[m.MovieID]; alreadyDownloading {
			continue
		}
		if searching[m.MovieID] {
			continue
		}
		cards = append(cards, render.DownloadCardView{
			ItemID: fmt.Sprintf("radarr-%d", m.MovieID),
			Title:  m.Title,
			Poster: fmt.Sprintf("/image/radarr/%d", m.MovieID),
			Status: "searching",
		})
		searching[m.MovieID] = true
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
	searching := make(map[int]bool, len(missing))
	for _, m := range missing {
		if _, alreadyDownloading := downloading[m.SeriesID]; alreadyDownloading {
			continue
		}
		if searching[m.SeriesID] {
			continue
		}
		cards = append(cards, render.DownloadCardView{
			ItemID: fmt.Sprintf("sonarr-%d", m.SeriesID),
			Title:  m.Title,
			Poster: fmt.Sprintf("/image/sonarr/%d", m.SeriesID),
			Status: "searching",
		})
		searching[m.SeriesID] = true
	}
	return cards, nil
}

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

// displayOrder ranks statuses for the Downloading section's card ordering:
// downloading cards lead (matches this section's original design — "what's
// actively happening" first), then non-downloading cards are ordered by how
// much attention they need (failed > stalled > importing > searching), with
// title/ItemID as final deterministic tiebreaks. This must stay a *total*
// order across every known status — a sort.Slice comparator that treats two
// different statuses as equal (returns false both directions) makes the
// final order depend on the pre-sort slice's order, which comes from
// non-deterministic Go map iteration in fetchRadarrCards/fetchSonarrCards.
var displayOrder = map[string]int{
	"downloading": 0,
	"failed":      1,
	"stalled":     2,
	"importing":   3,
	"searching":   4,
}

func sortDownloadCards(cards []render.DownloadCardView) {
	sort.Slice(cards, func(i, j int) bool {
		a, b := cards[i], cards[j]
		if displayOrder[a.Status] != displayOrder[b.Status] {
			return displayOrder[a.Status] < displayOrder[b.Status]
		}
		if a.Status == "downloading" && a.Percent != b.Percent {
			return a.Percent > b.Percent
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		return a.ItemID < b.ItemID
	})
}
