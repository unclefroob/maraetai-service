# Test Manifest: Web UI Motion & Visual Polish
**Scope tier:** micro
**Platforms:** web (`internal/web/static/`)
**Branch:** feature/web-ui-motion-polish
**PR:** https://github.com/unclefroob/maraetai-service/pull/28

## Surfaces touched
- Route navigation (`app.js` `route()`) — View Transitions API crossfade + fallback
- All list/grid loading states (`loading()`, `renderLibrary`, `renderGenre`, `renderMyMusic`, `runSearch`) — shimmer skeletons
- Album/song cards (`styles.css` `.card`) — hover lift + shadow
- Queue panel + lyrics modal (`player.js` `toggleQueue`/`toggleLyrics`) — animated open/close
- Ad-hoc dialogs (`promptDialog`/`confirmDialog`/`addToPlaylistDialog` in `app.js`) — animated open/close, auto-focus
- Now-playing bar (`player.js` `setAmbient`) — blurred ambient backdrop, crossfades on track change

## Acceptance flows

**AF-1: Route transition**
- Precondition: signed in, on Home.
- Steps: click Library, then Search, then a nav item back to Home.
- Expected: each change crossfades (no hard blank flash); nav highlight updates immediately; back/forward via browser history still works.

**AF-2: Skeleton loading**
- Precondition: throttle network (e.g. Chrome DevTools "Slow 3G").
- Steps: navigate to Library (Albums tab), then Genre → Songs, then My Music.
- Expected: a shimmering skeleton (cards for grid views, rows for list views) shows until data arrives, then swaps to real content — no flash of empty/blank state, no plain "Loading…" text.

**AF-3: Card hover**
- Steps: hover an album card on Home or Library.
- Expected: card lifts slightly with a deeper shadow; existing hover play-button reveal still works.

**AF-4: Queue panel / lyrics modal**
- Steps: open the queue panel (bottom-right icon), close it; open lyrics, close it. Repeat several times rapidly.
- Expected: both animate in/out consistently; ESC and click-outside still close them; no stuck/half-open state.

**AF-5: Dialog focus**
- Steps: click "+ New playlist", or rename/delete an existing playlist.
- Expected: dialog fades/scales in; the text field (where present) is focused automatically and ready to type into immediately.

**AF-6: Ambient backdrop**
- Steps: play a track, then skip to a few more tracks in sequence.
- Expected: the now-playing bar shows a blurred backdrop of the album art that crossfades on each track change; no extra visible network stall (reuses the already-loaded art).

**AF-7 (error/edge): Reduced motion**
- Precondition: enable "reduce motion" at the OS level.
- Steps: repeat AF-1 through AF-6.
- Expected: all transitions/animations collapse to near-instant; nothing breaks functionally.

**AF-8 (error/edge): No View Transitions API (Firefox)**
- Steps: repeat AF-1 in Firefox.
- Expected: navigation still works via the instant-swap fallback; no console errors.

## Setup
- A running Navidrome server behind `maraetai-service`, with valid credentials — no seed data beyond a normal library (a few albums/playlists) is required.

## Out of scope
- No visual redesign of color palette/layout/typography — motion/polish only.
- No automated test coverage added — this frontend has no test harness by design (`.autofeature/designs/web-player-admin-shell-2026-06-23.md`); verification is manual per above.
