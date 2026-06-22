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

- **M0 — Skeleton (this milestone):** transparent reverse proxy. Forwards all
  traffic to Navidrome unchanged, with streaming-safe pass-through for audio.
  Point an app at it and everything works exactly as before.

Planned:

- **M1** — tee `scrobble` into a SQLite play store (with metadata snapshot).
- **M2** — `getRecentlyPlayed` endpoint (XML + JSON) + forward-and-validate auth.
- **M3** — wire the recents call into the Maraetai apps.
- **M4** — embedded SPA: history timeline + Wrapped-style stats.

## How it works

```
app → proxy
        ├─ /healthz                       → served locally (no upstream hit)
        ├─ (M1) /rest/scrobble            → tee: log play, forward unchanged
        ├─ (M2) /rest/getRecentlyPlayed   → served from the play store
        └─ everything else                → streaming reverse proxy → Navidrome
```

New routes are registered ahead of the catch-all in
[`internal/proxy/proxy.go`](internal/proxy/proxy.go), so audio streaming and any
endpoint we don't explicitly handle are never on a special code path.
`FlushInterval = -1` ensures audio streams are piped, not buffered.

## Configuration

| Env var          | Required | Default  | Description                              |
| ---------------- | -------- | -------- | ---------------------------------------- |
| `NAVIDROME_URL`  | yes      | —        | Upstream Navidrome base URL              |
| `LISTEN_ADDR`    | no       | `:8080`  | Address the proxy listens on             |

## Run locally

```sh
NAVIDROME_URL=http://localhost:4533 go run .
# proxy now on http://localhost:8080 — point a Maraetai app there
```

## Run with Docker Compose (proxy + Navidrome sidecar)

```sh
cp .env.example .env   # set MUSIC_DIR to your library
docker compose up -d
# apps connect to http://<host>:8080
```

## Test

```sh
go test ./...
```

Covers transparent forwarding (path/query/Host/headers), local `/healthz`, and
incremental streaming (proving responses aren't buffered).
