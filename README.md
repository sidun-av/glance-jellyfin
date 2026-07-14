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
Glance's own `cache:` interval (see step 5 below) is all the freshness this needs.

## Setup

### 1. Create a Jellyfin API key

In Jellyfin: **Admin Dashboard → API Keys → +**. Give it a name (e.g. "glance-jellyfin") and copy
the key.

### 2. Find your Jellyfin user ID

The "Latest Items" endpoint this widget uses is scoped to a specific user's library
access/view. In Jellyfin: **Admin Dashboard → Users → (your user)** — the user ID is in the page's
URL (`.../userdetails?userId=<this-part>`).

### 3. Configure

Every setting can be set as an environment variable — no file to create or mount. Env vars always
take priority over `config.yml`, so the two approaches can be mixed if you want.

- `JELLYFIN_URL` — reachable from *this container* (e.g. `http://jellyfin:8096` over your
  Docker/LAN network).
- `JELLYFIN_TOKEN` — the API key from step 1.
- `JELLYFIN_USER_ID` — the user ID from step 2.
- `JELLYFIN_PUBLIC_URL` — Jellyfin's browser-facing base URL (e.g. `https://jellyfin.example.com`),
  used to build each poster's click-through link. Only needs to be reachable from *your browser*,
  not from this container.

See "Environment variable reference" below for the full list. If you'd rather hand-edit a file
instead, copy [`config.example.yml`](config.example.yml) to `config.yml`, mount it at `/config.yml`,
and skip the env vars it covers.

### 4. Run it alongside Glance

**Option A — Komodo (or any GUI stack manager that can pull a stack from a git repo):**

Point Komodo's Stack source at this repo (`sidun-av/glance-jellyfin`),
[`docker-compose.example.yml`](docker-compose.example.yml) as the compose file. Then set
`JELLYFIN_URL`/`JELLYFIN_TOKEN`/`JELLYFIN_USER_ID`/`JELLYFIN_PUBLIC_URL` (required) and any other
overrides you want in the stack's Environment tab — nothing to SSH in and edit. Add it to the same
Docker network as Jellyfin.

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
```

Add it to the same Docker network as Jellyfin.

### 5. Add the widget to Glance

```yaml
- type: extension
  url: http://glance-jellyfin:8080/widget
  cache: 30m
  allow-potentially-dangerous-html: true
```

`cache: 30m` is intentionally slow — a media library only needs to look fresh every so often, not
live-updated.

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
| `TITLE` | `title` | `Library` | Widget title shown in Glance |
| `LIMIT` | `limit` | `12` | Number of most-recently-added items to show |

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

## Out of scope (for now)

Browsing the full library / pagination, separate Movies/TV Shows tabs, year/rating/type badges on
cards, and live/real-time updates — see the design spec for the reasoning behind each.

## Development

```bash
go test ./...
docker build -t glance-jellyfin:dev .
```
