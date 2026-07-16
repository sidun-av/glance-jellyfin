package render

import "testing"

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}

func count(s, substr string) int {
	n := 0
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			n++
		}
	}
	return n
}

func TestRenderWidget_RendersOneCardPerItem(t *testing.T) {
	html := RenderWidget(WidgetData{Cards: []CardView{
		{Title: "The Sheep Detectives", ImageSrc: "/image/abc123", Href: "https://jellyfin.example/web/#/details?id=abc123"},
		{Title: "Another Show", ImageSrc: "/image/def456", Href: "https://jellyfin.example/web/#/details?id=def456"},
	}})
	if got := count(html, `class="jf-card"`); got != 2 {
		t.Errorf("card count = %d, want 2", got)
	}
	if !contains(html, `src="/image/abc123"`) {
		t.Errorf("html missing card 1's image src: %q", html)
	}
	if !contains(html, `href="https://jellyfin.example/web/#/details?id=abc123"`) {
		t.Errorf("html missing card 1's href: %q", html)
	}
	if !contains(html, "The Sheep Detectives") {
		t.Errorf("html missing card 1's title")
	}
}

func TestRenderWidget_EmptyCardsShowsEmptyMessage(t *testing.T) {
	html := RenderWidget(WidgetData{Cards: nil})
	if !contains(html, "jf-empty") {
		t.Errorf("html missing empty-state message: %q", html)
	}
	if contains(html, `class="jf-card"`) {
		t.Errorf("html has a card when Cards is empty: %q", html)
	}
}

func TestRenderWidget_EscapesTitleAndHref(t *testing.T) {
	html := RenderWidget(WidgetData{Cards: []CardView{
		{Title: `<script>alert(1)</script>`, ImageSrc: "/image/x", Href: `"><script>`},
	}})
	if contains(html, "<script>alert(1)</script>") {
		t.Errorf("title not escaped: %q", html)
	}
	if contains(html, `href="">`) {
		t.Errorf("href not escaped: %q", html)
	}
}

func TestRenderUnavailable_ShowsMessage(t *testing.T) {
	html := RenderUnavailable()
	if !contains(html, "Jellyfin unavailable") {
		t.Errorf("html = %q, want unavailable message", html)
	}
}

func TestRenderWidget_CardHasDistinctPlayLink(t *testing.T) {
	html := RenderWidget(WidgetData{Cards: []CardView{
		{
			Title:    "The Sheep Detectives",
			ImageSrc: "/image/jellyfin/abc123",
			Href:     "https://jellyfin.example/web/#/details?id=abc123",
			PlayHref: "https://jellyfin.example/web/#/video?id=abc123&serverId=srv1",
		},
	}})
	if !contains(html, `href="https://jellyfin.example/web/#/video?id=abc123&amp;serverId=srv1"`) {
		t.Errorf("html missing play href: %q", html)
	}
	// Both the details link and the play link must exist as distinct
	// elements — nesting an <a> inside another <a> is invalid HTML.
	if !contains(html, `href="https://jellyfin.example/web/#/details?id=abc123"`) {
		t.Errorf("html missing details href: %q", html)
	}
}

func TestRenderWidget_PlayButtonIsCenteredAndLarger(t *testing.T) {
	html := RenderWidget(WidgetData{Cards: []CardView{
		{Title: "X", ImageSrc: "/image/jellyfin/x", Href: "/x", PlayHref: "/play/x"},
	}})
	if !contains(html, "top:50%;left:50%;transform:translate(-50%,-50%)") {
		t.Errorf("play button CSS is not centered on the card: %q", html)
	}
	if !contains(html, "width:44px;height:44px") {
		t.Errorf("play button CSS is still the old small size: %q", html)
	}
}

func TestRenderWidget_DownloadingSectionOmittedWhenEmpty(t *testing.T) {
	html := RenderWidget(WidgetData{Cards: nil, Downloading: nil})
	if contains(html, "jf-dl-card") {
		t.Errorf("html has a downloading card when Downloading is empty: %q", html)
	}
}

func TestRenderWidget_RendersDownloadingCards(t *testing.T) {
	html := RenderWidget(WidgetData{Downloading: []DownloadCardView{
		{ItemID: "radarr-1", Title: "Downloading Movie", Poster: "/image/radarr/1", Status: "downloading", Percent: 42},
		{ItemID: "sonarr-2", Title: "Searching Show", Poster: "/image/sonarr/2", Status: "searching"},
	}})
	if count(html, "jf-dl-card") != 2 {
		t.Errorf("downloading card count wrong: %q", html)
	}
	if !contains(html, `data-item-id="radarr-1"`) {
		t.Errorf("html missing data-item-id for downloading card: %q", html)
	}
	if !contains(html, `data-status="downloading"`) {
		t.Errorf("html missing data-status=downloading: %q", html)
	}
	if !contains(html, "42%") {
		t.Errorf("html missing percentage text: %q", html)
	}
	if !contains(html, `data-item-id="sonarr-2"`) || !contains(html, `data-status="searching"`) {
		t.Errorf("html missing searching card markup: %q", html)
	}
	if !contains(html, "Searching") {
		t.Errorf("html missing 'Searching' label: %q", html)
	}
	if !contains(html, `[data-status="searching"] .jf-dl-pct{color:var(--color-text-highlight)}`) {
		t.Errorf("'Searching…' text isn't styled with a readable (non-subdued) color: %q", html)
	}
}

func TestRenderWidget_IncludesLiveBootstrapAttributes(t *testing.T) {
	html := RenderWidget(WidgetData{LiveURL: "/jellyfin-widget/live.json", PollIntervalMS: 12000})
	if !contains(html, `data-live-url="/jellyfin-widget/live.json"`) {
		t.Errorf("html missing data-live-url: %q", html)
	}
	if !contains(html, `data-poll-ms="12000"`) {
		t.Errorf("html missing data-poll-ms: %q", html)
	}
	if !contains(html, "onerror=") {
		t.Errorf("html missing onerror bootstrap trick: %q", html)
	}
}
