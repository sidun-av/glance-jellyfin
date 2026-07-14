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
