# Download Statuses + Seerr Search Card Design

## Goal

Two additions to the existing `glance-jellyfin` widget:

1. Replace the Downloading section's binary searching/downloading status with a
   5-state model (Searching, Downloading, Importing, Stalled, Failed) derived
   from fields Radarr/Sonarr's queue API already returns but this widget
   doesn't read yet, with an animated "in progress" indicator for the two
   open-ended states and distinct colors for the two problem states.
2. A persistent "Search movies" card, styled like a poster card, always last
   in the Library grid, linking to Seerr (Jellyseerr/Overseerr, already
   running in the user's servarr stack) — a static shortcut, not an API
   integration.

## Out of scope

- Showing Radarr/Sonarr's actual error/warning message text — only a status
  label (e.g. "Failed"), not the underlying reason.
- A percent number for Importing/Stalled/Failed — only the Downloading state
  shows a percentage; the others are point-in-time labels.
- Multiple or dynamic Seerr cards, or calling Seerr's API for anything —
  this is one static link, not a request/search integration.
- Making Seerr's config required. It must stay optional (empty = card simply
  doesn't render) — the previous feature's final review flagged making
  Radarr/Sonarr hard-required as a design tension with this widget's
  degrade-quietly philosophy; this feature must not repeat that mistake.

## Section 1: Richer download statuses

### Data model

`internal/radarr` and `internal/sonarr`'s `QueueItem` gain two fields parsed
from the same `/api/v3/queue` records already being read: `TrackedStatus`
(raw `trackedDownloadStatus`: `"ok"` / `"warning"` / `"error"`) and
`TrackedState` (raw `trackedDownloadState`, e.g. `"downloading"` /
`"importPending"` / `"importing"`). `MissingItem` is unchanged — items in
the wanted/missing list aren't in the queue at all, so they have no tracked
state; they stay `"searching"` exactly as today. As with every other
Radarr/Sonarr field this widget reads, the exact string values are a
best-known-good starting point, confirmed/adjusted against the live
instance during deployment — the established pattern for this integration,
not a new risk.

### Status derivation (poller.go)

A new `classifyStatus(trackedStatus, trackedState string) string` replaces
the poller's current hardcoded `"downloading"` for anything in the queue,
returning one of `"downloading"`, `"importing"`, `"stalled"`, `"failed"`.
Priority order (first match wins): `trackedStatus == "error"` → failed;
`trackedStatus == "warning"` → stalled; `trackedState` is `importPending` or
`importing` → importing; otherwise → downloading (today's behavior).

Sonarr's per-series aggregation (picking one card for a series with several
queued episodes) currently picks the highest-`Percent` episode. It now picks
by status severity first (failed > stalled > importing > downloading — the
episode needing the most attention represents the series), then by percent
among ties — this matches "surface what's wrong" rather than "surface
what's closest to done" as the primary signal, which is the whole point of
this feature.

### Render (internal/render/downloading.go)

`DownloadCardView.Status` is now one of the 5 values above. The progress bar
renders only for `"downloading"`; the other four are label-only. The
Searching/Importing labels get an animated ellipsis — implemented as a
fixed-text inline span whose `width` animates through discrete `steps()`
keyframes (a technique that only animates the ordinary, universally-animatable
`width` property, not the `content` property, so it works in any
CSS-animation-capable browser without needing bleeding-edge support).
Stalled/Failed render as static (non-animated) colored text — animating a
label for a state where nothing is actually progressing would be misleading.

Colors use `var(--token, fallback)` throughout (e.g.
`var(--color-warning, #e0a458)` for Stalled, `var(--color-negative,
#e05f5f)` for Failed) so they render correctly whether or not Glance's
active theme happens to define those exact token names.

### Live-update JavaScript

The existing bootstrap script (the `onerror`-image-tag trick, unchanged
mechanism) currently hardcodes `'Searching…'` as its fallback label for
anything that isn't `"downloading"`. It needs to know the new vocabulary: on
each `/live.json` poll, it sets a small label span's text to the right word
for the item's status (percentage for downloading, "Searching" /
"Importing" / "Stalled" / "Failed" otherwise) and updates the `data-status`
attribute — the animated-dots span itself is untouched by JS in either
direction; its visibility and animation are driven purely by CSS attribute
selectors keyed off `data-status`, so a live update can never clobber it.

## Section 2: Seerr search card

### Config

New optional `Config.Seerr.PublicURL` (`yaml: seerr.public_url`, `env:
SEERR_PUBLIC_URL`) — Seerr's browser-facing URL, exactly analogous in role
to `jellyfin.public_url` (a click-through link target), but for Seerr. No
validation error when empty — this is the only Seerr-related config, and
there's no API token because this integration makes no Seerr API calls at
all; the card is a static link.

### Render

`WidgetData` gains `SeerrURL string`. After rendering the real Library
cards, if `SeerrURL != ""`, one more static card renders: same
poster-card footprint (aspect ratio, rounded corners) as a real card, but
with a dashed border, a centered search-icon glyph in place of a poster
image, and a "Search movies" caption in place of a title — wrapped in a
link to `SeerrURL`, opened in a new tab like every other card.

### Wiring (main.go)

`widgetHandler` passes `SeerrURL: strings.TrimRight(a.cfg.Seerr.PublicURL,
"/")` into `render.WidgetData`. An empty `Seerr.PublicURL` means an empty
`SeerrURL`, which means `RenderWidget`'s conditional simply skips the card —
no poller involvement, no background fetch, nothing to degrade if Seerr is
down (there's nothing to check; it's a static configured link).

## Testing

- `internal/radarr` and `internal/sonarr`: parsing tests for the two new
  queue fields.
- `poller_test.go`: `classifyStatus` covers all four queue-derived outcomes
  plus the priority ordering (e.g. `trackedStatus: "error"` wins even if
  `trackedState` looks like `"importing"`); Sonarr aggregation's
  severity-then-percent tie-break gets a dedicated test.
- `internal/render`: markup assertions for all 5 statuses (bar present only
  for downloading; dots span present/animated only for
  searching/importing; correct color class for stalled/failed); Seerr card
  present iff `SeerrURL != ""`, always last, href escaped.
- `config_test.go`: `seerr.public_url` optional — absent yields no error and
  no forced default (same pattern as the top-level `public_url` field).
- `main_test.go`: end-to-end test that `widgetHandler` renders the Seerr
  card when configured, and omits it when not.
