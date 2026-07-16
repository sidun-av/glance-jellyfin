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

func TestPoller_SonarrMultipleMissingEpisodesDedupToOneSearchingCard(t *testing.T) {
	sr := fakeSonarrServer(
		`{"records":[]}`,
		`{"records":[
			{"seriesId":5,"series":{"title":"Some Show"}},
			{"seriesId":5,"series":{"title":"Some Show"}}
		]}`,
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
	if got[0].ItemID != "sonarr-5" || got[0].Status != "searching" {
		t.Errorf("got[0] = %+v, want {sonarr-5 ... searching}", got[0])
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
