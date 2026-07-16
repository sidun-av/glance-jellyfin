# glance-jellyfin

A [Glance](https://github.com/glanceapp/glance) extension widget that shows the most recently
added movies and TV shows from a Jellyfin library as a grid of poster cards — click a poster to
open it in Jellyfin's own web UI.

## How it works

A small Go HTTP service Glance calls on its own schedule (a Glance
[extension widget](https://github.com/glanceapp/glance/blob/main/docs/extensions.md)). On each
request it asks Jellyfin for the most recently added movies/TV shows
(`GET /Users/{userId}/Items/Latest`), renders them as a poster grid, and proxies each poster image
through its own `/image/{itemId}` endpoint so your Jellyfin API key never reaches the browser.
There's no live-update mechanism — a media library doesn't change on a 10-second cadence, so
Glance's own `cache:` interval (see step 6 below) is all the freshness this needs.

Two more pieces feed the widget: each Library card also gets a **Play**
button that deep-links straight into Jellyfin's web player (not just its
details page), and a **Downloading** section shows monitored-but-missing
movies/shows sourced from Radarr and Sonarr — a "Searching…" label for
items nothing's been grabbed for yet, and a live-updating progress bar
(polled from `/live.json` every 12s in the browser) for items actively
downloading. Radarr's and Sonarr's own `/api/v3/queue` already reports
download-client progress, so this widget never talks to qBittorrent or
Prowlarr directly.

## Setup

### 1. Create a Jellyfin API key

In Jellyfin: **Admin Dashboard → API Keys → +**. Give it a name (e.g. "glance-jellyfin") and copy
the key.

### 2. Find your Jellyfin user ID

The "Latest Items" endpoint this widget uses is scoped to a specific user's library
access/view. In Jellyfin: **Admin Dashboard → Users → (your user)** — the user ID is in the page's
URL (`.../userdetails?userId=<this-part>`).

### 3. Create a Radarr API key and a Sonarr API key

In Radarr: **Settings → General → Security** — copy the API Key. Do the same
in Sonarr. These stay server-side (this container's poller and image proxy
use them; the browser never does), so unlike the Jellyfin token above they
need no "public" browser-facing counterpart.

### 4. Expose this service to your browser, not just to Glance

Glance's own server calls `/widget` over your internal Docker network — that part just needs
`JELLYFIN_URL`/`JELLYFIN_TOKEN`/`JELLYFIN_USER_ID` below. But each poster's `<img>` is loaded by
the *browser*, so it needs its own route to this service's `/image/{id}`, reachable from wherever
you actually open Glance (locally and/or externally).

If you reverse-proxy Glance (e.g. NPMplus) on the same host/domain, add a location block that
proxies a path prefix to this container, and it'll work from both local and external URLs
automatically:

```
location /jellyfin-widget/ {
    proxy_pass http://glance-jellyfin:8080/;
}
```

Then set `public_url: /jellyfin-widget` in `config.yml` (see below). If you'd rather expose this
container on its own LAN port instead, set `public_url` to that full origin, e.g.
`http://192.168.1.50:8082`.

### 5. Configure

Every setting can be set as an environment variable — no file to create or mount. Env vars always
take priority over `config.yml`, so the two approaches can be mixed if you want.

- `JELLYFIN_URL` — reachable from *this container* (e.g. `http://jellyfin:8096` over your
  Docker/LAN network).
- `JELLYFIN_TOKEN` — the API key from step 1.
- `JELLYFIN_USER_ID` — the user ID from step 2.
- `JELLYFIN_PUBLIC_URL` — Jellyfin's browser-facing base URL (e.g. `https://jellyfin.example.com`),
  used to build each poster's click-through link. Only needs to be reachable from *your browser*,
  not from this container.
- `RADARR_URL` / `RADARR_TOKEN` — Radarr's base URL (reachable from this
  container, e.g. `http://radarr:7878`) and the API key from step 3.
- `SONARR_URL` / `SONARR_TOKEN` — same, for Sonarr (e.g. `http://sonarr:8989`).
- `PUBLIC_URL` — reachable from *your browser* (see step 4 above). Not Jellyfin's own URL — this
  is where *this service* is reachable from.

See "Environment variable reference" below for the full list. If you'd rather hand-edit a file
instead, copy [`config.example.yml`](config.example.yml) to `config.yml`, mount it at `/config.yml`,
and skip the env vars it covers.

### 6. Run it alongside Glance

**Option A — Komodo (or any GUI stack manager that can pull a stack from a git repo):**

Point Komodo's Stack source at this repo (`sidun-av/glance-jellyfin`),
[`docker-compose.example.yml`](docker-compose.example.yml) as the compose file. Then set
`JELLYFIN_URL`/`JELLYFIN_TOKEN`/`JELLYFIN_USER_ID`/`JELLYFIN_PUBLIC_URL` (required), `PUBLIC_URL`
(see step 3), and any other overrides you want in the stack's Environment tab — nothing to SSH in
and edit. Add it to the same Docker network as Jellyfin.

**Option B — plain `docker compose`:**

```yaml
services:
  glance-jellyfin:
    image: ghcr.io/sidun-av/glance-jellyfin:latest
    restart: unless-stopped
    environment:
      - JELLYFIN_URL=http://jellyfin:8096
      - JELLYFIN_TOKEN=${JELLYFIN_TOKEN}
      - JELLYFIN_USER_ID=${JELLYFIN_USER_ID}
      - JELLYFIN_PUBLIC_URL=https://jellyfin.example.com
      - PUBLIC_URL=/jellyfin-widget
```

Add it to the same Docker network as Jellyfin.

### 7. Add the widget to Glance

```yaml
- type: extension
  url: http://glance-jellyfin:8080/widget
  cache: 3m
  allow-potentially-dangerous-html: true
```

`cache: 3m` (down from the Library-only version's `30m`) because the
Downloading section's card set — a download starting or finishing — should
show up reasonably promptly. Progress *numbers* update independently via
`/live.json` every 12 seconds in the browser, so they stay live between
these 3-minute refreshes.

## Environment variable reference

Every one of these overrides the matching `config.yml` field when set to a non-empty value — set
just the ones you want to change (e.g. in Komodo's stack Environment tab) and leave the rest unset
to use the built-in default (or whatever `config.yml` has, if you're mounting one).

| Env var | `config.yml` field | Default | Description |
|---|---|---|---|
| `JELLYFIN_URL` | `jellyfin.url` | — (required) | Jellyfin base URL, reachable from this container |
| `JELLYFIN_TOKEN` | `jellyfin.token` | — (required) | Jellyfin API key |
| `JELLYFIN_USER_ID` | `jellyfin.user_id` | — (required) | The Jellyfin user whose library access the "Latest Items" call uses |
| `JELLYFIN_PUBLIC_URL` | `jellyfin.public_url` | — (required) | Jellyfin's browser-facing base URL, used for each poster's click-through link |
| `RADARR_URL` | `radarr.url` | — (required) | Radarr base URL, reachable from this container |
| `RADARR_TOKEN` | `radarr.token` | — (required) | Radarr API key |
| `SONARR_URL` | `sonarr.url` | — (required) | Sonarr base URL, reachable from this container |
| `SONARR_TOKEN` | `sonarr.token` | — (required) | Sonarr API key |
| `PUBLIC_URL` | `public_url` | `""` (site root) | Path or origin *this service* is reachable at from the browser, used to prefix poster `<img>` URLs |
| `TITLE` | `title` | `Library` | Widget title shown in Glance |
| `LIMIT` | `limit` | `12` | Number of most-recently-added items to show |
| `DOWNLOADING_LIMIT` | `downloading_limit` | `12` | Max cards shown in the Downloading section |

The service's own listen port and config-file path aren't `config.yml` fields — they're read from
the environment before any config is loaded, so they're always plain environment variables:

| Env var | Default | Description |
|---|---|---|
| `PORT` | `8080` | Port the HTTP server listens on |
| `CONFIG_PATH` | `/config.yml` | Path to the config file read at startup |

## Error handling

If Jellyfin is unreachable, the whole widget shows a single "Jellyfin unavailable" message instead
of Glance's generic widget-failed state. An item with no poster image is silently skipped rather
than shown broken. If a single poster fails to load after the widget already rendered, only that
card's image breaks (falls back to its alt text) — the rest of the grid is unaffected.

If Radarr or Sonarr is unreachable, the Downloading section simply omits
that source's cards (the rest of the widget, including the other source's
cards, is unaffected) — consistent with the rest of this widget's
philosophy of degrading quietly rather than surfacing a broken state.

## Out of scope (for now)

Browsing the full library / pagination, separate Movies/TV Shows tabs, and year/rating/type badges on
cards — see the design spec for the reasoning behind each.

Per-episode granularity for TV shows (a series with any missing/downloading
episode shows as one card), and talking to qBittorrent or Prowlarr directly
(Radarr/Sonarr's own queue already reports download-client progress) — see
the design spec for the reasoning.

## Development

```bash
go test ./...
docker build -t glance-jellyfin:dev .
```
