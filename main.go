package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sidun-av/glance-jellyfin/internal/jellyfin"
	"github.com/sidun-av/glance-jellyfin/internal/render"
)

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

	publicURL := strings.TrimRight(a.cfg.Jellyfin.PublicURL, "/")
	var cards []render.CardView
	for _, it := range items {
		if !it.HasImage {
			continue
		}
		cards = append(cards, render.CardView{
			Title:    it.Name,
			ImageSrc: "/image/" + it.ID,
			Href:     publicURL + "/web/#/details?id=" + it.ID,
		})
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, render.RenderWidget(render.WidgetData{Cards: cards}))
}

func (a *app) imageHandler(w http.ResponseWriter, r *http.Request) {
	itemID := strings.TrimPrefix(r.URL.Path, "/image/")
	if itemID == "" {
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
