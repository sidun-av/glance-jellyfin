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
