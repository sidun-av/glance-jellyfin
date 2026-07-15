# Play Button + Downloading Section Design

## Goal

Extend `glance-jellyfin` with two additions to the existing Library grid:

1. Each Library card (an item already in Jellyfin) gets a **Play** button that
   deep-links straight into Jellyfin's web player, instead of only linking to
   the details page.
2. A new **Downloading** section shows monitored-but-not-yet-available movies
   and TV shows, sourced from Radarr and Sonarr, each as a poster card with
   either a "Searching…" label or a live-updating download progress bar.

## Out of scope

- Talking to qBittorrent directly. Radarr's and Sonarr's own `/api/v3/queue`
  endpoints already report per-item download progress (`size`/`sizeleft`)
  proxied from whichever download client is configured — that's the only
  download-progress source this widget needs.
- Talking to Prowlarr. It's an indexer proxy behind Radarr/Sonarr; it has no
  per-item download or search status of its own.
- Cross-referencing Radarr/Sonarr items against Jellyfin by TMDb/IMDb/TVDB ID.
  See "Avoiding duplicate cards" below for why this isn't needed.
- Per-episode granularity for TV shows. A series with any missing or
  downloading episode shows as a single card (see "Sonarr aggregation").
- Making the Library section's own 30-minute cache faster. It stays as-is;
  only the Downloading section needs fresher data (see "Refresh cadence").

## Architecture

Two new internal client packages, `internal/radarr` and `internal/sonarr`,
each mirroring the shape of the existing `internal/jellyfin/client.go`: a
`New(baseURL, apiKey string) *Client` constructor, context-scoped fetch
methods returning typed structs, and a poster-fetch method returning the same
`ImageResult{Body, ContentType, StatusCode}` shape `jellyfin.Client` already
uses. They stay separate (not a shared generic "servarr client") — two ~100
line files following an established pattern beat one generic abstraction
built for exactly two call sites.

A new `downloadPoller` in `main.go` owns a background ticker (10s interval)
that calls both clients concurrently, merges their results into a
`DownloadStatus` snapshot, and stores it behind an `RWMutex`. Both the
`/widget` handler (for the initial server-rendered HTML) and a new
`/live.json` handler (for client-side polling) read this same snapshot — the
poller is the only thing that ever calls Radarr/Sonarr, so N open browser
tabs never multiply upstream API load.

If a poll fails (Radarr or Sonarr unreachable), the poller keeps serving the
last-known-good snapshot rather than clearing it, and logs the error —
consistent with the widget's existing philosophy of degrading quietly (e.g.
an item with no poster is silently skipped, not shown broken). If the very
first poll fails, that source's items are simply absent until a poll
succeeds.

## Avoiding duplicate cards

When a download finishes and Radarr/Sonarr import it, Radarr/Sonarr remove
that item from their own queue and from `wanted/missing` (it now has a file).
This means the Downloading section self-clears purely from Radarr/Sonarr's
own state — no need to cross-reference against what Jellyfin has indexed.
There's a brief window where an item could appear in both sections (imported
into the filesystem but not yet scanned into Jellyfin's library), which is
acceptable: worst case a title is briefly listed twice, self-correcting
within the Library section's next 30-minute refresh or the Downloading
section's ~10s poll, whichever notices first.

## Sonarr aggregation

Sonarr's queue and `wanted/missing` are per-episode, but Downloading cards
are per-series (matching the Library section's one-card-per-title style). One
card per series that has at least one non-available episode:

- If any episode for that series is in the queue (actively downloading),
  the card shows a progress bar using that episode's progress. If multiple
  episodes are downloading simultaneously, use the one with the highest
  percent complete.
- Otherwise, if the series has any monitored missing episode, the card shows
  "Searching…".

## Data model

```go
// internal/radarr or internal/sonarr — shape shared conceptually, not by code
type QueueItem struct {
    ID       int     // Radarr movie ID / Sonarr series ID
    Title    string
    Size     int64
    SizeLeft int64
}
type MissingItem struct {
    ID    int
    Title string
}
```

```go
// internal/render
type DownloadCardView struct {
    ItemID  string // e.g. "radarr-123", "sonarr-456" — used as data-item-id
    Title   string
    Poster  string // proxied poster URL, e.g. /image/radarr/123
    Status  string // "searching" or "downloading"
    Percent int    // 0 for "searching"
}
```

The `WidgetData` struct (`internal/render/grid.go`) gains a `Downloading
[]DownloadCardView` field alongside the existing `Cards []CardView`.

## Image proxying

Today's `/image/{id}` route only ever proxies Jellyfin posters. It becomes
`/image/{source}/{id}` where `source` is `jellyfin`, `radarr`, or `sonarr` —
this is an internal, server-generated URL scheme (nothing external links to
it), so changing its shape has no compatibility cost. `imageHandler` reads
the `source` segment and dispatches to the matching client's poster-fetch
method. The existing `validItemID` regex (`^[0-9a-fA-F-]+$`) already covers
Radarr/Sonarr's plain-integer IDs (digits are a subset of hex characters), so
it's reused unchanged. The existing `public_url`-prefix dual-registration for
`/image/` (bare path + `{public_url}/image/`) continues to cover this whole
subtree without change, since it matches on the `/image/` prefix, not the
full path.

## Play button

Jellyfin's web client supports a direct playback deep link:
`{jellyfin_public_url}/web/#/video?id={itemId}&serverId={serverId}`. The
`serverId` is fetched once via `GET /System/Info/Public` (unauthenticated,
returns the server's `Id` field) when the app starts, and cached for the
process lifetime — it cannot change without a Jellyfin restart. The exact
query-parameter format is confirmed against the user's live Jellyfin
instance during implementation (a manual verification step, not a guess left
in the code); if it doesn't match what their Jellyfin version expects, the
implementation task adjusts the URL template before merging.

`CardView` (`internal/render/grid.go`) gains a `PlayHref string` field. Each
Library card renders both its existing whole-card link to the details page
and a distinct "Play" button/icon linking to `PlayHref`.

## Live updates

`main.go` gains a `/live.json` handler (registered at both the bare path and
the `{public_url}`-prefixed path, exactly like `/image/`) that serializes the
poller's current `DownloadStatus` snapshot as JSON — no upstream calls, it
only reads the cached snapshot.

The rendered widget HTML always includes the progress-bar element for every
Downloading card (even "searching" ones, at 0% width and a `status-searching`
class), plus a small inline `<script>` that polls `{public_url}/live.json`
every 12 seconds, matches each response item to a DOM node by
`data-item-id`, and updates its status class, bar width, and percentage text
in place. New downloads starting or finishing (i.e. cards appearing or
disappearing entirely) only show up on the widget's next full Glance-cache
refresh — only in-place numeric/status updates happen live. This keeps the
client-side script simple (attribute/text patching only, no DOM
construction) while still making progress feel alive between refreshes.

## Refresh cadence

The Library section keeps its current recommended `cache: 30m` reasoning
(a library's contents don't change minute-to-minute). Because the widget
now also has a Downloading section whose card set (not just its numbers)
should appear reasonably promptly, the README's recommended Glance `cache:`
value for the whole widget drops to `3m` — structural changes (a new
download starting, one finishing) appear within 3 minutes; numeric progress
within the 3-minute window still updates live via `/live.json`.

## Configuration

New `Config` fields (`config.go`), following the exact existing pattern
(YAML field + env var override, env always wins):

| YAML field | Env var | Required | Description |
|---|---|---|---|
| `radarr.url` | `RADARR_URL` | yes | Radarr base URL, reachable from this container (e.g. `http://radarr:7878`) |
| `radarr.token` | `RADARR_TOKEN` | yes | Radarr API key (Settings → General → Security) |
| `sonarr.url` | `SONARR_URL` | yes | Sonarr base URL, reachable from this container (e.g. `http://sonarr:8989`) |
| `sonarr.token` | `SONARR_TOKEN` | yes | Sonarr API key (Settings → General → Security) |
| `downloading_limit` | `DOWNLOADING_LIMIT` | no, default `12` | Max cards shown in the Downloading section |

Radarr and Sonarr URLs need no `public_url` counterpart — unlike Jellyfin
poster images (which used to be fetched by the browser directly before the
`public_url` fix) or this widget's own poster proxy, the browser never talks
to Radarr/Sonarr directly; only the server-side poller and image proxy do,
over the same internal Docker network this container already reaches
`jellyfin:8096` through (Radarr and Sonarr live in the same `servarr` Komodo
stack as Jellyfin).

## Testing

Same conventions as the rest of the repo: `internal/radarr` and
`internal/sonarr` get a `client_test.go` with a `fakeRadarrServer` /
`fakeSonarrServer` (mirroring `fakeJellyfinServer`), covering queue parsing,
missing-item parsing, poster proxying, and non-OK upstream status. The
poller gets a test with fake clients verifying: snapshot merges both
sources, keeps last-known-good on a failed poll, and Sonarr aggregation
picks the highest-progress episode per series. `render` gets tests for the
new `DownloadCardView` rendering (searching vs downloading markup,
`data-item-id` present) and the Play button's presence/href on `CardView`.
`main_test.go` gets tests for `/live.json` (serves the poller snapshot,
reachable at both bare and `public_url`-prefixed paths) and the
`/image/{source}/{id}` routing (dispatches to the right client per source).
