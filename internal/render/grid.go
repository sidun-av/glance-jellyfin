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
}

type WidgetData struct {
	Cards []CardView
}

func styleBlock() string {
	return `<style>
	.jf-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(100px,1fr));gap:10px}
	.jf-card{display:block;color:inherit;text-decoration:none}
	.jf-poster{width:100%;aspect-ratio:2/3;object-fit:cover;border-radius:6px;display:block;background:var(--color-widget-background-highlight)}
	.jf-title{font-size:11px;color:var(--color-text-highlight);margin-top:4px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
	.jf-empty{color:var(--color-text-subdue);font-size:.85em;padding:8px 0}
	.jf-unavailable{color:var(--color-text-subdue);padding:12px 0}
</style>`
}

func RenderWidget(data WidgetData) string {
	var b strings.Builder
	b.WriteString(styleBlock())

	if len(data.Cards) == 0 {
		b.WriteString(`<div class="jf-empty">no recently added items found</div>`)
		return b.String()
	}

	b.WriteString(`<div class="jf-grid">`)
	for _, c := range data.Cards {
		fmt.Fprintf(&b,
			`<a class="jf-card" href="%s" target="_blank" rel="noopener"><img class="jf-poster" src="%s" alt="%s" loading="lazy"><div class="jf-title">%s</div></a>`,
			html.EscapeString(c.Href), html.EscapeString(c.ImageSrc), html.EscapeString(c.Title), html.EscapeString(c.Title),
		)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func RenderUnavailable() string {
	return styleBlock() + `<div class="jf-unavailable">Jellyfin unavailable</div>`
}
