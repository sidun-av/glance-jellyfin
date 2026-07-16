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
