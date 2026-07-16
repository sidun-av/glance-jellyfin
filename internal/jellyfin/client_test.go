package jellyfin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchLatest_ParsesItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/Users/test-user/Items/Latest" {
			t.Errorf("path = %s, want /Users/test-user/Items/Latest", r.URL.Path)
		}
		if got := r.Header.Get("X-Emby-Token"); got != "test-token" {
			t.Errorf("X-Emby-Token = %q, want %q", got, "test-token")
		}
		if got := r.URL.Query().Get("Limit"); got != "12" {
			t.Errorf("Limit query param = %q, want %q", got, "12")
		}
		if got := r.URL.Query().Get("GroupItems"); got != "false" {
			t.Errorf("GroupItems query param = %q, want %q", got, "false")
		}
		if got := r.URL.Query().Get("IncludeItemTypes"); got != "Movie,Series" {
			t.Errorf("IncludeItemTypes query param = %q, want %q", got, "Movie,Series")
		}
		fmt.Fprint(w, `[
			{"Id":"abc123","Name":"The Sheep Detectives","Type":"Series","ImageTags":{"Primary":"tag1"}},
			{"Id":"def456","Name":"No Poster Movie","Type":"Movie","ImageTags":{}}
		]`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token", "test-user")
	items, err := client.FetchLatest(context.Background(), 12)
	if err != nil {
		t.Fatalf("FetchLatest: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].ID != "abc123" || items[0].Name != "The Sheep Detectives" || !items[0].HasImage {
		t.Errorf("items[0] = %+v, want {abc123, The Sheep Detectives, HasImage=true}", items[0])
	}
	if items[1].HasImage {
		t.Errorf("items[1].HasImage = true, want false (no ImageTags.Primary)")
	}
}

func TestFetchLatest_EmptyLibrary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token", "test-user")
	items, err := client.FetchLatest(context.Background(), 12)
	if err != nil {
		t.Fatalf("FetchLatest: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestFetchLatest_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(server.URL, "test-token", "test-user")
	_, err := client.FetchLatest(context.Background(), 12)
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want it to mention status 500", err)
	}
}

func TestFetchLatest_MalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token", "test-user")
	_, err := client.FetchLatest(context.Background(), 12)
	if err == nil {
		t.Fatal("expected error for malformed response, got nil")
	}
}

func TestFetchImage_StreamsBodyAndContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Items/abc123/Images/Primary" {
			t.Errorf("path = %s, want /Items/abc123/Images/Primary", r.URL.Path)
		}
		if got := r.Header.Get("X-Emby-Token"); got != "test-token" {
			t.Errorf("X-Emby-Token = %q, want %q", got, "test-token")
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("fake-jpeg-bytes"))
	}))
	defer server.Close()

	client := New(server.URL, "test-token", "test-user")
	result, err := client.FetchImage(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("FetchImage: %v", err)
	}
	defer result.Body.Close()

	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}
	if result.ContentType != "image/jpeg" {
		t.Errorf("ContentType = %q, want image/jpeg", result.ContentType)
	}
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "fake-jpeg-bytes" {
		t.Errorf("body = %q, want %q", body, "fake-jpeg-bytes")
	}
}

func TestFetchImage_NonOKStatusReturnsStatusCodeNotError(t *testing.T) {
	// A 404 from Jellyfin (missing poster) is not a Go error — the caller
	// (main.go's imageHandler) decides what to do with a non-200
	// StatusCode.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := New(server.URL, "test-token", "test-user")
	result, err := client.FetchImage(context.Background(), "missing")
	if err != nil {
		t.Fatalf("FetchImage: %v", err)
	}
	defer result.Body.Close()
	if result.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", result.StatusCode)
	}
}
