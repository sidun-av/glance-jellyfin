package render

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
)

type DownloadCardView struct {
	ItemID  string
	Title   string
	Poster  string
	Status  string // "searching" or "downloading"
	Percent int
}

func renderDownloadingSection(items []DownloadCardView) string {
	var b strings.Builder
	b.WriteString(`<style>
	[data-item-id]{display:block}
	.jf-dl-status{margin-top:4px}
	.jf-dl-bar{height:4px;border-radius:2px;background:var(--color-widget-background-highlight);overflow:hidden}
	.jf-dl-status[data-status="searching"] .jf-dl-bar{display:none}
	.jf-dl-fill{height:100%;background:var(--color-primary)}
	.jf-dl-pct{font-size:10px;color:var(--color-text-subdue);margin-top:2px}
</style>`)
	b.WriteString(`<div class="jf-section-label">Downloading</div><div class="jf-grid">`)
	for _, item := range items {
		pctText := "Searching&hellip;"
		width := 0
		if item.Status == "downloading" {
			pctText = fmt.Sprintf("%d%%", item.Percent)
			width = item.Percent
		}
		fmt.Fprintf(&b,
			`<div class="jf-dl-card" data-item-id="%s"><img class="jf-poster" src="%s" alt="%s" loading="lazy"><div class="jf-title">%s</div><div class="jf-dl-status" data-status="%s"><div class="jf-dl-bar"><div class="jf-dl-fill" style="width:%d%%"></div></div><div class="jf-dl-pct">%s</div></div></div>`,
			html.EscapeString(item.ItemID), html.EscapeString(item.Poster), html.EscapeString(item.Title),
			html.EscapeString(item.Title), html.EscapeString(item.Status), width, pctText,
		)
	}
	b.WriteString(`</div>`)
	return b.String()
}

type liveItem struct {
	ItemID  string `json:"item_id"`
	Status  string `json:"status"`
	Percent int    `json:"percent"`
}

type livePayload struct {
	Items []liveItem `json:"items"`
}

// RenderDownloadingLive builds the /live.json payload from the same
// DownloadCardView data used to render the widget, so live updates always
// match one source of truth.
func RenderDownloadingLive(items []DownloadCardView) ([]byte, error) {
	payload := livePayload{Items: []liveItem{}}
	for _, it := range items {
		payload.Items = append(payload.Items, liveItem{ItemID: it.ItemID, Status: it.Status, Percent: it.Percent})
	}
	return json.Marshal(payload)
}
