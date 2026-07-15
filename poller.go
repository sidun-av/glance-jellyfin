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
