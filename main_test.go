package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeJellyfinServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/Users/test-user/Items/Latest":
			fmt.Fprint(w, `[
				{"Id":"abc123","Name":"The Sheep Detectives","Type":"Series","ImageTags":{"Primary":"tag1"}},
				{"Id":"def456","Name":"No Poster Movie","Type":"Movie","ImageTags":{}}
			]`)
		case r.Method == http.MethodGet && r.URL.Path == "/Items/abc123/Images/Primary":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("fake-jpeg-bytes"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func testConfig(jellyfinURL string) *Config {
	return &Config{
		Jellyfin: JellyfinConfig{
			URL:       jellyfinURL,
			Token:     "test-token",
			UserID:    "test-user",
			PublicURL: "https://jellyfin.example.com",
		},
		Title: "Library",
		Limit: 12,
	}
}

func TestWidgetHandler_EndToEnd(t *testing.T) {
	jf := fakeJellyfinServer(t)
	defer jf.Close()

	cfg := testConfig(jf.URL)
	mux := newMux(cfg, newApp(cfg))

	req := httptest.NewRequest(http.MethodGet, "/widget", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Widget-Title") != "Library" {
		t.Errorf("Widget-Title = %q, want Library", rec.Header().Get("Widget-Title"))
	}
	if rec.Header().Get("Widget-Content-Type") != "html" {
		t.Errorf("Widget-Content-Type = %q, want html", rec.Header().Get("Widget-Content-Type"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, "The Sheep Detectives") {
		t.Errorf("body missing item title")
	}
	if !strings.Contains(body, `src="/image/abc123"`) {
		t.Errorf("body missing image proxy src")
	}
	if !strings.Contains(body, `href="https://jellyfin.example.com/web/#/details?id=abc123"`) {
		t.Errorf("body missing click-through href")
	}
	if strings.Contains(body, "No Poster Movie") {
		t.Errorf("body includes an item with no poster image, want it skipped")
	}
}

func TestWidgetHandler_JellyfinUnavailable(t *testing.T) {
	jf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer jf.Close()

	cfg := testConfig(jf.URL)
	mux := newMux(cfg, newApp(cfg))

	req := httptest.NewRequest(http.MethodGet, "/widget", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (service owns its degraded state)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Jellyfin unavailable") {
		t.Errorf("body = %s, want unavailable message", rec.Body.String())
	}
}

func TestImageHandler_ProxiesImageBytes(t *testing.T) {
	jf := fakeJellyfinServer(t)
	defer jf.Close()

	cfg := testConfig(jf.URL)
	mux := newMux(cfg, newApp(cfg))

	req := httptest.NewRequest(http.MethodGet, "/image/abc123", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "public, max-age=86400" {
		t.Errorf("Cache-Control = %q, want public, max-age=86400", rec.Header().Get("Cache-Control"))
	}
	if rec.Body.String() != "fake-jpeg-bytes" {
		t.Errorf("body = %q, want fake-jpeg-bytes", rec.Body.String())
	}
}

func TestImageHandler_MissingImageReturns404(t *testing.T) {
	jf := fakeJellyfinServer(t)
	defer jf.Close()

	cfg := testConfig(jf.URL)
	mux := newMux(cfg, newApp(cfg))

	req := httptest.NewRequest(http.MethodGet, "/image/does-not-exist", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHealthzHandler(t *testing.T) {
	cfg := testConfig("http://unused")
	mux := newMux(cfg, newApp(cfg))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
