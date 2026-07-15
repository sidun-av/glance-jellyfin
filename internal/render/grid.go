package render

import (
	"fmt"
	"html"
	"strings"
)

type CardView struct {
	Title    string
	ImageSrc string
	Href     string
	PlayHref string
}

type WidgetData struct {
	Cards          []CardView
	Downloading    []DownloadCardView
	LiveURL        string
	PollIntervalMS int
}

func styleBlock() string {
	return `<style>
	.jf-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(100px,1fr));gap:10px}
	.jf-card{position:relative;display:block}
	.jf-card-link{display:block;color:inherit;text-decoration:none}
	.jf-poster{width:100%;aspect-ratio:2/3;object-fit:cover;border-radius:6px;display:block;background:var(--color-widget-background-highlight)}
	.jf-title{font-size:11px;color:var(--color-text-highlight);margin-top:4px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
	.jf-empty{color:var(--color-text-subdue);font-size:.85em;padding:8px 0}
	.jf-unavailable{color:var(--color-text-subdue);padding:12px 0}
	.jf-play-btn{position:absolute;top:6px;right:6px;display:flex;align-items:center;justify-content:center;width:24px;height:24px;border-radius:50%;background:rgba(0,0,0,.6);color:#fff;text-decoration:none;font-size:11px;line-height:1}
	.jf-play-btn:hover{background:rgba(0,0,0,.8)}
	.jf-section-label{font-size:11px;text-transform:uppercase;letter-spacing:.04em;color:var(--color-text-subdue);margin:14px 0 6px}
</style>`
}

// bootstrapScript runs via an onerror attribute (see RenderWidget) because
// Glance mounts extension widget HTML with element.innerHTML, and <script>
// elements inserted that way are inert per the HTML spec — onerror/onload
// content attributes are not. It only ever patches data-* attributes and
// text content on cards that already exist in the initial render; it never
// adds or removes cards (that only happens on Glance's next full-page
// fetch).
const bootstrapScript = `(function(img){var root=img.closest('.jf-widget');if(!root)return;var url=root.dataset.liveUrl;var interval=parseInt(root.dataset.pollMs,10)||12000;function applyState(data){(data.items||[]).forEach(function(item){var card=root.querySelector('.jf-dl-card[data-item-id="'+CSS.escape(item.item_id)+'"]');if(!card)return;var status=card.querySelector('.jf-dl-status');if(!status)return;status.dataset.status=item.status;var fill=status.querySelector('.jf-dl-fill');if(fill)fill.style.width=item.percent+'%';var pct=status.querySelector('.jf-dl-pct');if(pct)pct.textContent=item.status==='downloading'?item.percent+'%':'Searching…';});}function poll(){fetch(url,{cache:'no-store'}).then(function(r){return r.ok?r.json():null;}).then(function(data){if(data)applyState(data);}).catch(function(){});}setInterval(poll,interval);poll();})(this)`

func RenderWidget(data WidgetData) string {
	var b strings.Builder
	b.WriteString(styleBlock())

	fmt.Fprintf(&b, `<div class="jf-widget" data-live-url="%s" data-poll-ms="%d">`,
		html.EscapeString(data.LiveURL), data.PollIntervalMS)

	if len(data.Cards) == 0 {
		b.WriteString(`<div class="jf-empty">no recently added items found</div>`)
	} else {
		b.WriteString(`<div class="jf-grid">`)
		for _, c := range data.Cards {
			b.WriteString(renderCard(c))
		}
		b.WriteString(`</div>`)
	}

	if len(data.Downloading) > 0 {
		b.WriteString(renderDownloadingSection(data.Downloading))
	}

	// Only include bootstrap script if there's a live URL to poll
	if data.LiveURL != "" {
		fmt.Fprintf(&b, `<img src="x" alt="" style="display:none;width:0;height:0" onerror="%s">`, html.EscapeString(bootstrapScript))
	}
	b.WriteString(`</div>`)
	return b.String()
}

func renderCard(c CardView) string {
	var b strings.Builder
	b.WriteString(`<div class="jf-card">`)
	fmt.Fprintf(&b,
		`<a class="jf-card-link" href="%s" target="_blank" rel="noopener"><img class="jf-poster" src="%s" alt="%s" loading="lazy"><div class="jf-title">%s</div></a>`,
		html.EscapeString(c.Href), html.EscapeString(c.ImageSrc), html.EscapeString(c.Title), html.EscapeString(c.Title),
	)
	if c.PlayHref != "" {
		fmt.Fprintf(&b, `<a class="jf-play-btn" href="%s" target="_blank" rel="noopener" aria-label="Play">&#9654;</a>`, html.EscapeString(c.PlayHref))
	}
	b.WriteString(`</div>`)
	return b.String()
}

func RenderUnavailable() string {
	return styleBlock() + `<div class="jf-unavailable">Jellyfin unavailable</div>`
}
