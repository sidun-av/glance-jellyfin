// poller_test.go
package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sidun-av/glance-jellyfin/internal/radarr"
	"github.com/sidun-av/glance-jellyfin/internal/render"
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

func TestPoller_SonarrAggregatesBySeverityThenPercent_RecordsReversed(t *testing.T) {
	// Same scenario as TestPoller_SonarrAggregatesBySeverityThenPercent, but
	// with the two queue records in the opposite order. fetchSonarrCards
	// processes records in whatever order the JSON (and, in production, Go
	// map iteration further downstream) hands them, so the failed episode
	// must win regardless of which record is seen first.
	sr := fakeSonarrServer(
		`{"records":[
			{"seriesId":5,"size":1000,"sizeleft":900,"trackedDownloadStatus":"error","trackedDownloadState":"failed","series":{"title":"Some Show"}},
			{"seriesId":5,"size":1000,"sizeleft":100,"trackedDownloadStatus":"ok","trackedDownloadState":"downloading","series":{"title":"Some Show"}}
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
		t.Errorf("Status = %q, want failed (severity beats percent, regardless of record order)", got[0].Status)
	}
}

func TestSortDownloadCards_TiesBreakByItemIDForDeterminism(t *testing.T) {
	// Two "downloading" cards with identical Percent, and two "searching"
	// cards with identical Title: without a final ItemID tiebreaker,
	// sort.Slice (not a stable sort) can order these differently between
	// calls, since the pre-sort order comes from Go map iteration
	// (non-deterministic) in fetchRadarrCards/fetchSonarrCards.
	cards := []render.DownloadCardView{
		{ItemID: "radarr-2", Status: "downloading", Percent: 50},
		{ItemID: "radarr-1", Status: "downloading", Percent: 50},
		{ItemID: "sonarr-2", Status: "searching", Title: "Same Title"},
		{ItemID: "sonarr-1", Status: "searching", Title: "Same Title"},
	}
	sortDownloadCards(cards)

	want := []string{"radarr-1", "radarr-2", "sonarr-1", "sonarr-2"}
	got := make([]string, len(cards))
	for i, c := range cards {
		got[i] = c.ItemID
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v (ties must break by ItemID)", got, want)
		}
	}
}

func TestSortDownloadCards_TotalOrderAcrossAllStatuses(t *testing.T) {
	// Four cards, each a different non-downloading status ("failed",
	// "stalled", "importing", "searching"). The old comparator only ever
	// compared a status against "downloading", so for two DIFFERENT
	// non-downloading statuses it returned false in both directions —
	// violating sort.Slice's less-function contract and leaving their
	// relative order dependent on the pre-sort slice's order, which comes
	// from non-deterministic Go map iteration in
	// fetchRadarrCards/fetchSonarrCards. Feeding two different permutations
	// of the same cards must produce the identical final order.
	permA := []render.DownloadCardView{
		{ItemID: "radarr-1", Status: "failed", Title: "Failed Movie"},
		{ItemID: "radarr-2", Status: "stalled", Title: "Stalled Movie"},
		{ItemID: "radarr-3", Status: "importing", Title: "Importing Movie"},
		{ItemID: "radarr-4", Status: "searching", Title: "Searching Movie"},
	}
	permB := []render.DownloadCardView{
		{ItemID: "radarr-4", Status: "searching", Title: "Searching Movie"},
		{ItemID: "radarr-3", Status: "importing", Title: "Importing Movie"},
		{ItemID: "radarr-1", Status: "failed", Title: "Failed Movie"},
		{ItemID: "radarr-2", Status: "stalled", Title: "Stalled Movie"},
	}
	sortDownloadCards(permA)
	sortDownloadCards(permB)

	want := []string{"radarr-1", "radarr-2", "radarr-3", "radarr-4"}
	gotA := make([]string, len(permA))
	for i, c := range permA {
		gotA[i] = c.ItemID
	}
	gotB := make([]string, len(permB))
	for i, c := range permB {
		gotB[i] = c.ItemID
	}
	for i := range want {
		if gotA[i] != want[i] {
			t.Fatalf("permutation A order = %v, want %v (failed > stalled > importing > searching)", gotA, want)
		}
		if gotB[i] != want[i] {
			t.Fatalf("permutation B order = %v, want %v (failed > stalled > importing > searching)", gotB, want)
		}
	}
}
