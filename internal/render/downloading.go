package render

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
)

// DownloadCardView.Status is one of "searching", "downloading",
// "importing", "stalled", or "failed".
type DownloadCardView struct {
	ItemID  string
	Title   string
	Poster  string
	Status  string
	Percent int
}

func renderDownloadingSection(items []DownloadCardView) string {
	var b strings.Builder
	b.WriteString(`<style>
	[data-item-id]{display:block}
	.jf-dl-status{margin-top:4px}
	.jf-dl-bar{height:4px;border-radius:2px;background:var(--color-widget-background-highlight);overflow:hidden}
	.jf-dl-status:not([data-status="downloading"]) .jf-dl-bar{display:none}
	.jf-dl-fill{height:100%;background:var(--color-primary)}
	.jf-dl-pct{font-size:10px;color:var(--color-text-subdue);margin-top:2px}
	.jf-dl-status[data-status="searching"] .jf-dl-pct{color:var(--color-text-highlight)}
	.jf-dl-status[data-status="importing"] .jf-dl-pct{color:var(--color-text-highlight)}
	.jf-dl-status[data-status="stalled"] .jf-dl-pct{color:var(--color-warning,#e0a458)}
	.jf-dl-status[data-status="failed"] .jf-dl-pct{color:var(--color-negative,#e05f5f)}
	.jf-dl-dots{display:none;width:0;overflow:hidden;vertical-align:bottom}
	.jf-dl-status[data-status="searching"] .jf-dl-dots{display:inline-block;animation:jf-dl-dots 1.4s steps(4) infinite}
	.jf-dl-status[data-status="importing"] .jf-dl-dots{display:inline-block;animation:jf-dl-dots 1.4s steps(4) infinite}
	@keyframes jf-dl-dots{to{width:1.6em}}
</style>`)
	b.WriteString(`<div class="jf-section-label">Downloading</div><div class="jf-grid">`)
	for _, item := range items {
		label := statusLabel(item)
		width := 0
		if item.Status == "downloading" {
			width = item.Percent
		}
		fmt.Fprintf(&b,
			`<div class="jf-dl-card" data-item-id="%s"><img class="jf-poster" src="%s" alt="%s" loading="lazy"><div class="jf-title">%s</div><div class="jf-dl-status" data-status="%s"><div class="jf-dl-bar"><div class="jf-dl-fill" style="width:%d%%"></div></div><div class="jf-dl-pct"><span class="jf-dl-label">%s</span><span class="jf-dl-dots">....</span></div></div></div>`,
			html.EscapeString(item.ItemID), html.EscapeString(item.Poster), html.EscapeString(item.Title),
			html.EscapeString(item.Title), html.EscapeString(item.Status), width, html.EscapeString(label),
		)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// statusLabel returns the short text shown for a card's current status:
// a live percentage while downloading, otherwise a fixed word per status.
func statusLabel(item DownloadCardView) string {
	switch item.Status {
	case "downloading":
		return fmt.Sprintf("%d%%", item.Percent)
	case "importing":
		return "Importing"
	case "stalled":
		return "Stalled"
	case "failed":
		return "Failed"
	default: // "searching"
		return "Searching"
	}
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
