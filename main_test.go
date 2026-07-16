package main

import (
	"context"
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
	if !strings.Contains(body, `src="/image/jellyfin/abc123"`) {
		t.Errorf("body missing image proxy src")
	}
	if !strings.Contains(body, `href="https://jellyfin.example.com/web/#/details?id=abc123"`) {
		t.Errorf("body missing click-through href")
	}
	if strings.Contains(body, "No Poster Movie") {
		t.Errorf("body includes an item with no poster image, want it skipped")
	}
}

func TestWidgetHandler_ImageSrcPrefixedByPublicURL(t *testing.T) {
	// The rendered <img src> is fetched by the BROWSER, not this container —
	// if this service sits behind a reverse proxy at a path prefix (the
	// same situation glance-homeassistant's PUBLIC_URL solves for
	// /live.json), the src must include that prefix or the browser's
	// request lands on whatever else owns the site root instead of this
	// service.
	jf := fakeJellyfinServer(t)
	defer jf.Close()

	cfg := testConfig(jf.URL)
	cfg.PublicURL = "/jellyfin-widget"
	mux := newMux(cfg, newApp(cfg))

	req := httptest.NewRequest(http.MethodGet, "/widget", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `src="/jellyfin-widget/image/jellyfin/abc123"`) {
		t.Errorf("body = %q, want image src prefixed with public_url", body)
	}
}

func TestImageHandler_ReachableAtPublicURLPrefix(t *testing.T) {
	// Mirrors glance-homeassistant's equivalent test: some reverse proxies
	// forward the full original path instead of stripping the location
	// prefix, so this service must answer at both "/image/{id}" and
	// "{public_url}/image/{id}" regardless of which one the proxy sends.
	jf := fakeJellyfinServer(t)
	defer jf.Close()

	cfg := testConfig(jf.URL)
	cfg.PublicURL = "/jellyfin-widget"
	mux := newMux(cfg, newApp(cfg))

	req := httptest.NewRequest(http.MethodGet, "/jellyfin-widget/image/jellyfin/abc123", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (must be reachable whether or not the reverse proxy strips the public_url prefix)", rec.Code)
	}
	if rec.Body.String() != "fake-jpeg-bytes" {
		t.Errorf("body = %q, want fake-jpeg-bytes", rec.Body.String())
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

	req := httptest.NewRequest(http.MethodGet, "/image/jellyfin/abc123", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/image/jellyfin/does-not-exist", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestImageHandler_RejectsPathTraversalItemID(t *testing.T) {
	// A hit-counting server, not fakeJellyfinServer: fakeJellyfinServer's
	// default case already returns 404 for unrecognized paths, which would
	// make a test that only asserts rec.Code == 404 pass whether or not the
	// itemID validation exists (confirmed: without the fix, the traversal
	// path is spliced unescaped into the outbound URL and reaches this
	// server unnormalized as "/Items/../secret/Images/Primary", which the
	// fake server's default case would 404 anyway). Counting hits here
	// proves the request never left main.go at all.
	var hitCount int
	jf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer jf.Close()

	cfg := testConfig(jf.URL)
	mux := newMux(cfg, newApp(cfg))

	// %2e%2e%2Fsecret is the escaped form of "../secret". net/http.ServeMux
	// (Go 1.22+) routes on the escaped path, so this still matches "/image/",
	// but the handler reads r.URL.Path, which net/url decodes to
	// "/image/../secret" — confirmed directly below: httptest.NewRequest
	// parses the target the same way a real incoming request would, and
	// req.URL.Path for this target is "/image/../secret" while
	// req.URL.EscapedPath() remains "/image/%2e%2e%2Fsecret". Without the
	// itemID allow-list, TrimPrefix would yield itemID == "../secret",
	// which FetchImage splices unescaped into the outbound Jellyfin request
	// URL.
	req := httptest.NewRequest(http.MethodGet, "/image/jellyfin/%2e%2e%2Fsecret", nil)
	if req.URL.Path != "/image/jellyfin/../secret" {
		t.Fatalf("test setup invalid: req.URL.Path = %q, want %q (this test doesn't exercise the decoded-path scenario it's meant to)", req.URL.Path, "/image/jellyfin/../secret")
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (path-traversal-shaped itemID must be rejected)", rec.Code)
	}
	if hitCount != 0 {
		t.Fatalf("fake Jellyfin server was hit %d time(s), want 0 (itemID must be rejected before any outbound request is made)", hitCount)
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
