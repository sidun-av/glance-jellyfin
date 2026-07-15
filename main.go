package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/sidun-av/glance-jellyfin/internal/jellyfin"
	"github.com/sidun-av/glance-jellyfin/internal/render"
)

// validItemID matches Jellyfin item IDs, which are hex-GUID-shaped (hex
// digits, optionally hyphenated). Anything else is rejected before it can
// reach the outbound Jellyfin request URL in client.FetchImage, which
// splices itemID into that URL unescaped.
var validItemID = regexp.MustCompile(`^[0-9a-fA-F-]+$`)

type app struct {
	cfg    *Config
	client *jellyfin.Client
}

func newApp(cfg *Config) *app {
	return &app{cfg: cfg, client: jellyfin.New(cfg.Jellyfin.URL, cfg.Jellyfin.Token, cfg.Jellyfin.UserID)}
}

func (a *app) widgetHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	w.Header().Set("Widget-Title", a.cfg.Title)
	w.Header().Set("Widget-Content-Type", "html")

	items, err := a.client.FetchLatest(ctx, a.cfg.Limit)
	if err != nil {
		log.Printf("jellyfin unavailable: %v", err)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, render.RenderUnavailable())
		return
	}

	jellyfinPublicURL := strings.TrimRight(a.cfg.Jellyfin.PublicURL, "/")
	imagePrefix := strings.TrimRight(a.cfg.PublicURL, "/")
	var cards []render.CardView
	for _, it := range items {
		if !it.HasImage {
			continue
		}
		cards = append(cards, render.CardView{
			Title:    it.Name,
			ImageSrc: imagePrefix + "/image/" + it.ID,
			Href:     jellyfinPublicURL + "/web/#/details?id=" + it.ID,
		})
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, render.RenderWidget(render.WidgetData{Cards: cards}))
}

func (a *app) imageHandler(w http.ResponseWriter, r *http.Request) {
	// This handler is reachable at both "/image/{id}" and
	// "{public_url}/image/{id}" (see newMux) — strip whichever prefix is
	// actually present, since a reverse proxy may or may not have
	// stripped public_url before forwarding.
	path := r.URL.Path
	if prefix := strings.TrimRight(a.cfg.PublicURL, "/"); prefix != "" && strings.HasPrefix(path, prefix) {
		path = strings.TrimPrefix(path, prefix)
	}
	itemID := strings.TrimPrefix(path, "/image/")
	if !validItemID.MatchString(itemID) {
		// Empty itemID and path-traversal/otherwise-malformed itemIDs are
		// both rejected here, with the same 404 as a genuinely missing
		// image, so this check isn't an oracle for probing valid vs.
		// invalid IDs.
		w.WriteHeader(http.StatusNotFound)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := a.client.FetchImage(ctx, itemID)
	if err != nil {
		log.Printf("fetch image %s: %v", itemID, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	defer result.Body.Close()

	if result.StatusCode != http.StatusOK {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, result.Body)
}

func newMux(cfg *Config, a *app) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("/widget", a.widgetHandler)
	mux.HandleFunc("/image/", a.imageHandler)

	// A reverse proxy in front of this service may forward a Custom
	// Location's full original path instead of stripping the public_url
	// prefix (depends on proxy configuration details not every proxy UI
	// makes easy to get right — see glance-homeassistant's README/history
	// for the concrete failure mode this defends against). Registering
	// the image handler under that prefix too means poster images work
	// either way. Only applies when public_url is itself a path — a full
	// origin is a distinct listener reached directly, with no such prefix
	// ever attached.
	if prefix := strings.TrimRight(cfg.PublicURL, "/"); strings.HasPrefix(prefix, "/") {
		mux.HandleFunc(prefix+"/image/", a.imageHandler)
	}
	return mux
}

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/config.yml"
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	a := newApp(cfg)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, newMux(cfg, a)))
}
