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

## What it adds

All of these are built from a SQLite **play store**: `scrobble` requests are
tee'd into it (then forwarded untouched), with song metadata resolved once via
upstream `getSong`, cached, and snapshotted per play so history survives
upstream edits/deletes. Recording is async and never blocks playback.

- **Recently played songs** — `getRecentlyPlayed`: a cross-device, de-duplicated,
  most-recent-first song list. The gap that started this: the Subsonic API and
  Navidrome only expose recently-played *albums*.
- **On Repeat** — `getOnRepeat`: your most-replayed *songs* (default: ≥3 plays in
  the last 30 days), with a non-standard `playCount` attribute.
- **Songs for you** — `getSongsForYou`: a personalized daily mix from your play
  history (your rotation + similar-artist discovery via `getArtistInfo2` +
  serendipity), each track tagged with a `reason`. Falls back to favourites +
  random for a new listener.
- **Fast artist play** — `getArtistSongs`: a whole discography in one client
  round-trip; the proxy does the per-album fan-out server-side, concurrently.
- **Listening stats + web app** — `GET /api/stats` (totals, top artists/songs,
  plays-by-day) and an embedded single-page app at **`/app/`** (history +
  Wrapped-style stats), served from the same binary.

**Backwards compatible by design.** Everything standard is forwarded to
Navidrome untouched, so a plain Subsonic client works through the proxy exactly
as before. The Maraetai apps gate the extensions behind an explicit *server
type* setting and fall back to standard behaviour on a plain server.

**Auth model.** The proxy stores no passwords and keeps no user table — its own
endpoints **forward-and-validate**: the request's own `u`/`t`/`s` are checked
against upstream `ping`, so results are scoped to the requesting user and
Navidrome stays the source of truth. Responses are Subsonic-shaped (XML default,
JSON, JSONP) and use Subsonic error codes (HTTP 200 with an in-body status), as
clients expect.

## How it works

```
app → proxy
        ├─ /healthz                       → served locally (no upstream hit)
        ├─ /app/                          → embedded single-page web app
        ├─ /api/stats                     → JSON listening stats (auth via upstream ping)
        ├─ /rest/scrobble[.view]          → tee: record play, forward unchanged
        ├─ /rest/getRecentlyPlayed[.view] → served from the play store (auth via upstream ping)
        ├─ /rest/getOnRepeat[.view]       → most-replayed songs (song-level On Repeat, from the play store)
        ├─ /rest/getSongsForYou[.view]    → personalized daily mix (rotation + similar-artist discovery), from play history
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
docker run -d --name maraetai-service --restart unless-stopped \
  --network <your-navidrome-network> \
  -e NAVIDROME_URL=http://<navidrome-container>:4533 \
  -p 4534:4534 -v maraetai-data:/data \
  ghcr.io/unclefroob/maraetai-service:latest
# point the Maraetai apps at http://<host>:4534
```

The proxy doesn't probe Navidrome at startup (it forwards lazily, per request),
so start order doesn't matter — bring it up before or after Navidrome and it
works once Navidrome is reachable. The image's `HEALTHCHECK` reports readiness.

**Persistence.** The image runs as a non-root user (uid 65532) and stores the
play-history DB at `/data/maraetai.db` (baked-in `DB_PATH`). Keep the
`maraetai-data` volume mounted — omit it and your play history is lost on every
recreate. A fresh volume gets the right owner automatically; if you attach a
volume previously written as root, fix its ownership once:

```sh
docker run --rm -v maraetai-data:/data alpine chown -R 65532:65532 /data
```

### Updating

The image republishes to GHCR on every push to `main`. To move a running
container to the latest (the named volume keeps your history):

```sh
docker pull ghcr.io/unclefroob/maraetai-service:latest
docker rm -f maraetai-service
# then re-run the same `docker run …` as above
```

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
