# maraetai-service

A transparent reverse proxy that sits in front of a [Navidrome](https://www.navidrome.org/)
server to extend it with features the Subsonic API doesn't provide — starting
with **cross-device play history** (a real Navidrome gap: there's no
server-side "recently played songs" list).

The [Maraetai](https://github.com/unclefroob/maraetai) apps (iOS / Android /
macOS) point their server URL at this proxy instead of at Navidrome directly.
Most traffic is forwarded untouched; the proxy only intervenes on the handful
of endpoints it augments, and adds new Subsonic-shaped endpoints for new
features. A single binary also serves an embedded SPA (history + stats).

## Status

- **M0 — Skeleton:** transparent reverse proxy. Forwards all traffic to
  Navidrome unchanged, with streaming-safe pass-through for audio. Point an app
  at it and everything works exactly as before.
- **M1 — Play-history capture (current):** `scrobble` requests are tee'd into a
  SQLite play store before being forwarded untouched. Song metadata (which
  scrobbles don't carry — only an id + time) is resolved once via upstream
  `getSong`, cached, and snapshotted on each play so history survives upstream
  edits/deletes. `submission=false` now-playing pings are forwarded but not
  recorded. Recording is asynchronous and never blocks the playback path.

- **M2 — Recently-played read API (current):** `GET /rest/getRecentlyPlayed[.view]`
  serves the play store as a Subsonic-shaped response (XML default, JSON, JSONP),
  de-duplicated to one entry per song (most recent play), most-recent-first, with
  `count`/`offset` paging and a non-standard `playedAt` attribute. Auth is
  **forward-and-validate**: the request's own `u`/`t`/`s` are checked against
  upstream `ping`, so results are scoped to the requesting user and no
  credentials or user table live here. Errors use Subsonic codes (40 wrong
  creds, 10 missing param) with HTTP 200, as Subsonic clients expect.

- **M4 — Embedded SPA + stats API (current):** a single-page web app served from
  the binary at `/app/` (history timeline + Wrapped-style stats), backed by a new
  `GET /api/stats` JSON endpoint that aggregates the play store (totals, top
  artists, top songs, plays-by-day). The SPA authenticates with Navidrome
  credentials using salt+token (the password never goes on the wire), the same
  way the native apps do.

Planned:

- **M3** — wire the recents call into the Maraetai apps.

## How it works

```
app → proxy
        ├─ /healthz                       → served locally (no upstream hit)
        ├─ /app/                          → embedded single-page web app
        ├─ /api/stats                     → JSON listening stats (auth via upstream ping)
        ├─ /rest/scrobble[.view]          → tee: record play, forward unchanged
        ├─ /rest/getRecentlyPlayed[.view] → served from the play store (auth via upstream ping)
        ├─ /rest/getArtistSongs[.view]    → whole-discography fan-out done server-side (1 client round-trip)
        └─ everything else                → streaming reverse proxy → Navidrome
```

New routes are registered ahead of the catch-all in
[`internal/proxy/proxy.go`](internal/proxy/proxy.go), so audio streaming and any
endpoint we don't explicitly handle are never on a special code path.
`FlushInterval = -1` ensures audio streams are piped, not buffered.

## Configuration

| Env var          | Required | Default  | Description                              |
| ---------------- | -------- | -------- | ---------------------------------------- |
| `NAVIDROME_URL`  | yes      | —                  | Upstream Navidrome base URL    |
| `LISTEN_ADDR`    | no       | `:4534`            | Address the proxy listens on   |
| `DB_PATH`        | no       | `./data/maraetai.db` | SQLite play-history database  |

## Run locally

```sh
NAVIDROME_URL=http://localhost:4533 go run .
# proxy now on http://localhost:4534 — point a Maraetai app there
# web app at http://localhost:4534/app/ (sign in with Navidrome credentials)
```

The SPA is plain ES modules + CSS embedded via `go:embed` (no build step / no
node toolchain) and lives in `internal/web/static`.

## Docker

The service ships as a single static image (`gcr.io/distroless/static`), published
to GHCR on every push to `main`:

```
ghcr.io/unclefroob/maraetai-service:latest
```

It needs nothing but a reachable Navidrome — set `NAVIDROME_URL` and run it. There
is no database to provision (SQLite file in a mounted volume) and no Navidrome
config to touch; standard Subsonic traffic is forwarded untouched, so a plain
Navidrome client keeps working through it too.

### In front of a Navidrome you already run

`docker-compose.existing.yml` joins your existing Navidrome's Docker network and
reaches it by container name — nothing about your Navidrome setup changes:

```sh
# tell it how to reach your Navidrome (by container/service name, not localhost)
# and which Docker network that container is on:
export NAVIDROME_URL=http://navidrome:4533
export NAVIDROME_NETWORK=navidrome_default     # see the file's header to find yours
docker compose -f docker-compose.existing.yml up -d
# point the Maraetai apps at http://<host>:4534
```

Or without compose:

```sh
docker run -d --name maraetai-service \
  --network <your-navidrome-network> \
  -e NAVIDROME_URL=http://<navidrome-container>:4533 \
  -p 4534:4534 -v maraetai-data:/data \
  ghcr.io/unclefroob/maraetai-service:latest
```

The proxy doesn't probe Navidrome at startup (it forwards lazily, per request),
so start order doesn't matter — bring it up before or after Navidrome and it
works once Navidrome is reachable. The image's `HEALTHCHECK` reports readiness.

### All-in-one (proxy + a fresh Navidrome)

For a new setup, `docker-compose.yml` runs both:

```sh
cp .env.example .env   # set MUSIC_DIR to your library
docker compose up -d
# apps connect to http://<host>:4534
```

## Test

```sh
go test ./...
```

Covers transparent forwarding (path/query/Host/headers), local `/healthz`, and
incremental streaming (proving responses aren't buffered).
