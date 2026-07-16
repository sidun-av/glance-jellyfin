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
	"sync"
	"time"

	"github.com/sidun-av/glance-jellyfin/internal/jellyfin"
	"github.com/sidun-av/glance-jellyfin/internal/radarr"
	"github.com/sidun-av/glance-jellyfin/internal/render"
	"github.com/sidun-av/glance-jellyfin/internal/sonarr"
)

// validItemID matches Jellyfin/Radarr/Sonarr item IDs: Jellyfin's are
// hex-GUID-shaped, Radarr/Sonarr's are plain integers (a subset of hex
// digits), so one allow-list covers all three. Anything else is rejected
// before it can reach an outbound request URL, which splices it in
// unescaped.
var validItemID = regexp.MustCompile(`^[0-9a-fA-F-]+$`)

const downloadPollInterval = 10 * time.Second
const liveClientPollMS = 12000

type app struct {
	cfg            *Config
	jellyfinClient *jellyfin.Client
	radarrClient   *radarr.Client
	sonarrClient   *sonarr.Client
	poller         *downloadPoller

	serverIDMu sync.Mutex
	serverID   string
}

func newApp(cfg *Config) *app {
	jellyfinClient := jellyfin.New(cfg.Jellyfin.URL, cfg.Jellyfin.Token, cfg.Jellyfin.UserID)
	radarrClient := radarr.New(cfg.Radarr.URL, cfg.Radarr.Token)
	sonarrClient := sonarr.New(cfg.Sonarr.URL, cfg.Sonarr.Token)
	poller := newDownloadPoller(radarrClient, sonarrClient, cfg.DownloadingLimit)
	poller.Start(context.Background(), downloadPollInterval)

	return &app{
		cfg:            cfg,
		jellyfinClient: jellyfinClient,
		radarrClient:   radarrClient,
		sonarrClient:   sonarrClient,
		poller:         poller,
	}
}

func liveURL(publicURL string) string {
	return strings.TrimRight(publicURL, "/") + "/live.json"
}

// fetchServerIDCached fetches Jellyfin's server ID (needed for the Play
// deep link) at most once per process: on failure it returns "" and leaves
// nothing cached, so the next request retries rather than being stuck
// without a Play link until a restart.
func (a *app) fetchServerIDCached(ctx context.Context) string {
	a.serverIDMu.Lock()
	defer a.serverIDMu.Unlock()
	if a.serverID != "" {
		return a.serverID
	}
	id, err := a.jellyfinClient.FetchServerID(ctx)
	if err != nil {
		log.Printf("fetch jellyfin server id: %v", err)
		return ""
	}
	a.serverID = id
	return a.serverID
}

func (a *app) widgetHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	w.Header().Set("Widget-Title", a.cfg.Title)
	w.Header().Set("Widget-Content-Type", "html")

	items, err := a.jellyfinClient.FetchLatest(ctx, a.cfg.Limit)
	if err != nil {
		log.Printf("jellyfin unavailable: %v", err)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, render.RenderUnavailable())
		return
	}

	jellyfinPublicURL := strings.TrimRight(a.cfg.Jellyfin.PublicURL, "/")
	imagePrefix := strings.TrimRight(a.cfg.PublicURL, "/")
	serverID := a.fetchServerIDCached(ctx)

	var cards []render.CardView
	for _, it := range items {
		if !it.HasImage {
			continue
		}
		href := jellyfinPublicURL + "/web/#/details?id=" + it.ID
		playHref := href
		if serverID != "" {
			playHref = jellyfinPublicURL + "/web/#/video?id=" + it.ID + "&serverId=" + serverID
		}
		cards = append(cards, render.CardView{
			Title:    it.Name,
			ImageSrc: imagePrefix + "/image/jellyfin/" + it.ID,
			Href:     href,
			PlayHref: playHref,
		})
	}

	downloadingCards := a.poller.Snapshot()
	for i := range downloadingCards {
		downloadingCards[i].Poster = imagePrefix + downloadingCards[i].Poster
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, render.RenderWidget(render.WidgetData{
		Cards:          cards,
		Downloading:    downloadingCards,
		SeerrURL:       strings.TrimRight(a.cfg.Seerr.PublicURL, "/"),
		LiveURL:        liveURL(a.cfg.PublicURL),
		PollIntervalMS: liveClientPollMS,
	}))
}

func (a *app) liveHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	body, err := render.RenderDownloadingLive(a.poller.Snapshot())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

func (a *app) imageHandler(w http.ResponseWriter, r *http.Request) {
	// This handler is reachable at both "/image/{source}/{id}" and
	// "{public_url}/image/{source}/{id}" (see newMux) — strip whichever
	// prefix is actually present, since a reverse proxy may or may not have
	// stripped public_url before forwarding.
	path := r.URL.Path
	if prefix := strings.TrimRight(a.cfg.PublicURL, "/"); prefix != "" && strings.HasPrefix(path, prefix) {
		path = strings.TrimPrefix(path, prefix)
	}
	rest := strings.TrimPrefix(path, "/image/")
	source, itemID, found := strings.Cut(rest, "/")
	if !found || !validItemID.MatchString(itemID) {
		// Empty/malformed itemIDs, path-traversal-shaped itemIDs, and a
		// missing source segment are all rejected here with the same 404 as
		// a genuinely missing image, so this check isn't an oracle for
		// probing valid vs. invalid IDs.
		w.WriteHeader(http.StatusNotFound)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var (
		body        io.ReadCloser
		contentType string
		statusCode  int
	)
	switch source {
	case "jellyfin":
		result, err := a.jellyfinClient.FetchImage(ctx, itemID)
		if err != nil {
			log.Printf("fetch jellyfin image %s: %v", itemID, err)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, contentType, statusCode = result.Body, result.ContentType, result.StatusCode
	case "radarr":
		result, err := a.radarrClient.FetchPoster(ctx, itemID)
		if err != nil {
			log.Printf("fetch radarr poster %s: %v", itemID, err)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, contentType, statusCode = result.Body, result.ContentType, result.StatusCode
	case "sonarr":
		result, err := a.sonarrClient.FetchPoster(ctx, itemID)
		if err != nil {
			log.Printf("fetch sonarr poster %s: %v", itemID, err)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, contentType, statusCode = result.Body, result.ContentType, result.StatusCode
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}
	defer body.Close()

	if statusCode != http.StatusOK {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, body)
}

func newMux(cfg *Config, a *app) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("/widget", a.widgetHandler)
	mux.HandleFunc("/live.json", a.liveHandler)
	mux.HandleFunc("/image/", a.imageHandler)

	// A reverse proxy in front of this service may forward a Custom
	// Location's full original path instead of stripping the public_url
	// prefix (depends on proxy configuration details not every proxy UI
	// makes easy to get right — see glance-homeassistant's README/history
	// for the concrete failure mode this defends against). Registering the
	// image and live-json handlers under that prefix too means they work
	// either way. Only applies when public_url is itself a path — a full
	// origin is a distinct listener reached directly, with no such prefix
	// ever attached.
	if prefix := strings.TrimRight(cfg.PublicURL, "/"); strings.HasPrefix(prefix, "/") {
		mux.HandleFunc(prefix+"/image/", a.imageHandler)
		mux.HandleFunc(prefix+"/live.json", a.liveHandler)
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
