# Feature Brief — Web player + admin shell (maraetai-service SPA)

Mode: AUTOMATED. Decisions locked with user:
- Scope: **Player + admin shell together** (web player MVP + admin-gated section reusing existing stats).
- Manage users: **link out to Navidrome's own admin UI** (proxy has no user store).

## Problem
The `/app/` SPA is only a history + Wrapped-stats viewer (~565 LOC vanilla JS). We want
parity-MVP with the mobile apps: a real **web player** (browse + play), plus an
**admin** area (history/stats today; user mgmt = link-out).

## Scope (v1 — this PR)
Web player (vanilla JS, no build step, served at /app/, Navidrome-cred auth — all reused):
- Home: On Repeat, Songs for you, Recently played, Recently added, Most played (existing proxy endpoints).
- Library: Albums (paged grid), Artists, Playlists, Favourites.
- Search (search3): artists / albums / songs.
- Detail views: album, artist, playlist → track lists.
- Playback: HTML5 <audio> stream, queue, play/pause/prev/next/seek/volume, now-playing bar with art.
  Scrobble on threshold (tee'd by the proxy → populates the play store).
- Star / unstar.
- Admin (shown only when getUser.adminRole): the current history timeline + /api/stats,
  plus "Manage users in Navidrome ↗" external link.

## Out of scope (phased)
Lyrics, downloads/offline, queue drag-reorder, gapless/crossfade, casting, in-app user CRUD.

## Architecture
- New small modules: `api.js` (auth+endpoints, extracted from app.js), `player.js` (audio engine+queue+scrobble), `app.js` (router+views+admin), expanded `index.html`/`styles.css`. `md5.js` kept.
- Server: new `GET /api/config` → `{ navidromeUrl }` (from optional `NAVIDROME_PUBLIC_URL`) so the admin link-out has a target; falls back to guidance text when unset. No auth (URL isn't secret).
- Admin role via Subsonic `getUser`.
- No new client/server data model; everything rides existing Subsonic + maraetai endpoints + the scrobble tee.

## Tests (Go side; JS has no harness by design — no build step)
- web serves index.html at /app/.
- /api/config returns JSON with the configured URL (and empty when unset).
- existing suite stays green.

## Risks
- Navidrome /app shadowing: maraetai SPA owns /app, so Navidrome's UI isn't reachable through the proxy → user-mgmt is an external link to NAVIDROME_PUBLIC_URL (documented).
- Large vanilla-JS surface, no JS unit tests → manual smoke + Go serve test.
