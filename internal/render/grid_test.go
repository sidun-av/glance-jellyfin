package render

import (
	"strings"
	"testing"
)

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

func TestRenderWidget_RendersImportingStalledFailedStatuses(t *testing.T) {
	html := RenderWidget(WidgetData{Downloading: []DownloadCardView{
		{ItemID: "radarr-1", Title: "Importing Movie", Poster: "/p/1", Status: "importing"},
		{ItemID: "radarr-2", Title: "Stalled Movie", Poster: "/p/2", Status: "stalled"},
		{ItemID: "radarr-3", Title: "Failed Movie", Poster: "/p/3", Status: "failed"},
	}})
	if !contains(html, `data-status="importing"`) || !contains(html, "Importing") {
		t.Errorf("html missing importing card markup: %q", html)
	}
	if !contains(html, `data-status="stalled"`) || !contains(html, "Stalled") {
		t.Errorf("html missing stalled card markup: %q", html)
	}
	if !contains(html, `data-status="failed"`) || !contains(html, "Failed") {
		t.Errorf("html missing failed card markup: %q", html)
	}
}

func TestRenderWidget_ProgressBarOnlyShownForDownloading(t *testing.T) {
	html := RenderWidget(WidgetData{Downloading: []DownloadCardView{
		{ItemID: "radarr-1", Title: "M", Poster: "/p/1", Status: "downloading", Percent: 55},
	}})
	if !contains(html, `.jf-dl-status:not([data-status="downloading"]) .jf-dl-bar{display:none}`) {
		t.Errorf("CSS doesn't hide the bar for every non-downloading status: %q", html)
	}
}

func TestRenderWidget_StalledAndFailedHaveDistinctColors(t *testing.T) {
	html := RenderWidget(WidgetData{Downloading: []DownloadCardView{
		{ItemID: "radarr-1", Title: "M", Poster: "/p/1", Status: "stalled"},
	}})
	if !contains(html, `[data-status="stalled"] .jf-dl-pct{color:var(--color-warning,#e0a458)}`) {
		t.Errorf("stalled status isn't styled with the warning color: %q", html)
	}
	if !contains(html, `[data-status="failed"] .jf-dl-pct{color:var(--color-negative,#e05f5f)}`) {
		t.Errorf("failed status isn't styled with the negative color: %q", html)
	}
}

func TestRenderWidget_AnimatedDotsOnlyForSearchingAndImporting(t *testing.T) {
	html := RenderWidget(WidgetData{Downloading: []DownloadCardView{
		{ItemID: "radarr-1", Title: "M", Poster: "/p/1", Status: "searching"},
	}})
	if !contains(html, `class="jf-dl-dots"`) {
		t.Errorf("html missing the animated-dots span: %q", html)
	}
	if !contains(html, `[data-status="searching"] .jf-dl-dots{display:inline-block;animation:jf-dl-dots`) {
		t.Errorf("dots aren't animated for searching: %q", html)
	}
	if !contains(html, `[data-status="importing"] .jf-dl-dots{display:inline-block;animation:jf-dl-dots`) {
		t.Errorf("dots aren't animated for importing: %q", html)
	}
	if !contains(html, `@keyframes jf-dl-dots`) {
		t.Errorf("html missing the dots keyframes: %q", html)
	}
}

func TestRenderWidget_BootstrapScriptKnowsAllStatusLabels(t *testing.T) {
	html := RenderWidget(WidgetData{LiveURL: "/live.json", PollIntervalMS: 12000})
	// RenderWidget embeds bootstrapScript inside an onerror="..." attribute via
	// html.EscapeString, which turns every ' into &#39; (required so the
	// script's own embedded " characters, e.g. inside the CSS.escape
	// selector, don't break out of the attribute). So the rendered output
	// contains the HTML-entity-escaped form of the labels object, not the
	// raw JS source.
	for _, want := range []string{"searching:&#39;Searching&#39;", "importing:&#39;Importing&#39;", "stalled:&#39;Stalled&#39;", "failed:&#39;Failed&#39;"} {
		if !contains(html, want) {
			t.Errorf("bootstrap script missing label mapping %q: %q", want, html)
		}
	}
	if contains(html, "Searching…") {
		t.Errorf("bootstrap script still has the old hardcoded ellipsis fallback: %q", html)
	}
}

func TestRenderWidget_SeerrCardAppearsWhenConfigured(t *testing.T) {
	html := RenderWidget(WidgetData{
		Cards:    []CardView{{Title: "A Movie", ImageSrc: "/image/jellyfin/1", Href: "/x"}},
		SeerrURL: "https://seerr.example.com",
	})
	if !contains(html, `class="jf-seerr-card"`) {
		t.Errorf("html missing seerr card: %q", html)
	}
	if !contains(html, `href="https://seerr.example.com"`) {
		t.Errorf("html missing seerr card href: %q", html)
	}
	if !contains(html, "Search movies") {
		t.Errorf("html missing seerr card caption: %q", html)
	}
}

func TestRenderWidget_SeerrCardAbsentWhenNotConfigured(t *testing.T) {
	html := RenderWidget(WidgetData{
		Cards: []CardView{{Title: "A Movie", ImageSrc: "/image/jellyfin/1", Href: "/x"}},
	})
	if contains(html, "jf-seerr-card") {
		t.Errorf("html has a seerr card when SeerrURL is empty: %q", html)
	}
}

func TestRenderWidget_SeerrCardIsLastInGrid(t *testing.T) {
	html := RenderWidget(WidgetData{
		Cards: []CardView{
			{Title: "First", ImageSrc: "/image/jellyfin/1", Href: "/1"},
			{Title: "Second", ImageSrc: "/image/jellyfin/2", Href: "/2"},
		},
		SeerrURL: "https://seerr.example.com",
	})
	lastCard := strings.LastIndex(html, `class="jf-card"`)
	seerrCard := strings.Index(html, `class="jf-seerr-card"`)
	if seerrCard < lastCard {
		t.Errorf("seerr card is not positioned after the real cards: %q", html)
	}
}

func TestRenderWidget_SeerrCardShowsEvenWhenLibraryEmpty(t *testing.T) {
	html := RenderWidget(WidgetData{Cards: nil, SeerrURL: "https://seerr.example.com"})
	if contains(html, "jf-empty") {
		t.Errorf("html shows the empty-library message even though the seerr card should render: %q", html)
	}
	if !contains(html, `class="jf-seerr-card"`) {
		t.Errorf("html missing seerr card when library is empty: %q", html)
	}
}

func TestRenderWidget_SeerrCardEscapesURL(t *testing.T) {
	html := RenderWidget(WidgetData{SeerrURL: `"><script>alert(1)</script>`})
	if contains(html, `<script>alert(1)</script>`) {
		t.Errorf("seerr URL not escaped: %q", html)
	}
}
