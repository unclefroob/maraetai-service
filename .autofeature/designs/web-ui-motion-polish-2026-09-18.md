# Feature: Web UI Motion & Visual Polish
**Date:** 2026-09-18
**Branch:** feature/web-ui-motion-polish
**Stack:** VANILLA_WEB (no framework, no bundler — three ES modules `app.js`/`player.js`/`api.js`, `go:embed`-served static assets; closest existing specialist is `react-architect`, but it does not apply — no React in this repo)
**Status:** Draft
**Mode:** AUTOMATED
**Model tier:** sonnet for every spawn, no escalation (project preference — hobby app, see memory `maraetai-model-preference`)

## Context (from Explore scan)
- `.autofeature/patterns.md`: none.
- Prior design brief `.autofeature/designs/web-player-admin-shell-2026-06-23.md` established this SPA deliberately with **no build step / no node toolchain** — that constraint stays in force.
- Sibling design brief `.autofeature/designs/now-playing-ambient-video-background-2026-09-18.md` gave the native macOS/iOS app a new ambient mesh-gradient/blurred-art background motif — a natural cross-platform visual cue to echo on web.
- `internal/web/web.go` serves `internal/web/static/*` via `//go:embed`; `Dockerfile` copies the repo as-is and `go build`s — any HTML/CSS/JS edit ships purely by rebuilding the Go binary. No test harness for this frontend, by design.
- Current UI: a hand-built Spotify-dark-theme clone (pure black bg, `#1db954` green accent, rounded system font, 3-region app shell: sidebar/content/now-playing bar). Views are swapped by replacing `#view`'s `innerHTML` wholesale on hash-route change — instant, no transition. Only 4 isolated micro-transitions exist in 339 lines of CSS (hover opacity fades, one toast slide, one hover reveal, one lyric-line color fade); no `@keyframes`, no reusable duration/easing tokens, no skeleton loading (plain "Loading…" text), no `prefers-reduced-motion` handling.

## Problem
The webapp is functionally solid but visually reads as generic/utilitarian rather than a polished, shippable product: route changes are a hard instant swap, loading states are bare text, and the only motion in the app is a handful of one-off opacity fades with hardcoded durations. There's no motion system, so nothing feels intentional or "designed" — it doesn't yet match the bar the sibling native apps are setting with their new ambient visual language.

## Solution
Add a lightweight motion design system to `styles.css` (CSS custom-property tokens for easing/duration, `@keyframes`, `prefers-reduced-motion` handling) and wire it into the three existing JS modules at the points where state already changes: route swaps, modal/panel open-close, loading states, and hover/press feedback. Use only browser-native primitives (CSS transitions/animations, the View Transitions API with a CSS-only fallback, `IntersectionObserver` if needed for scroll reveals) — nothing that requires a bundler, a framework, or a new dependency, preserving the repo's explicit no-build-step constraint. Echo the native app's new ambient blurred-art motif in the now-playing bar for cross-platform visual consistency.

## User Story
As a listener using the web player, I want view changes, loading states, and interactions to feel smooth and intentional (not an instant flash) so that the app feels like a finished, professional product rather than a functional prototype.

## Scope: IN
- Motion token system in `styles.css`: `--ease-standard`, `--ease-emphasized`, `--duration-fast/base/slow` custom properties; replace the 4 existing hardcoded-duration transitions to use the tokens.
- Animated route/view transitions in `app.js`'s `route()` dispatcher: `document.startViewTransition()` when supported, graceful instant-swap fallback when not (Firefox, older Safari) — navigation must never break.
- Skeleton/shimmer loading state (CSS `@keyframes shimmer`) replacing the plain "Loading…" text for shelves, lists, and detail views while a fetch is in flight.
- Animated open/close for the queue panel and lyrics modal in `player.js` (currently instant show/hide via class toggle) — slide/fade in with the token easings, matching close.
- Hover/press micro-interaction polish: album-art card lift+scale+shadow on hover, button press feedback, sidebar nav active-state transition — all via the new tokens, no new markup.
- Ambient blurred-album-art backdrop behind the now-playing bar, crossfading on track change — cheap CSS `filter: blur()` on the already-loaded art, no extra network requests.
- `@media (prefers-reduced-motion: reduce)` block that suppresses/shortens all of the above to near-instant.

## Scope: OUT
- No JS framework, virtual DOM, or bundler/build step — the repo's no-build-step design is a deliberate constraint from the original SPA brief, not a gap to close.
- No color palette, typography, or layout/structure redesign — this is a motion/polish pass on the existing Spotify-dark-theme shell, not a rebrand.
- No changes to the native macOS/iOS/Android apps (separate repos) — the ambient motif is borrowed visually, not shared code.
- No new automated visual-regression/E2E tooling for this frontend — none exists today, by design; verification stays manual (Test Scenarios below).

## Existing Code to Touch
- `internal/web/static/styles.css`: add motion tokens, `@keyframes` (shimmer, panel/modal enter-exit, ambient crossfade), rewrite the 4 existing transitions onto tokens, add card/button/nav hover-press rules, add the reduced-motion media query.
- `internal/web/static/app.js`: wrap the `route()` view swap in `document.startViewTransition()` (feature-detected) with instant fallback; render skeleton markup into `#view` before a route's data fetch resolves, swap to real content on resolve.
- `internal/web/static/player.js`: toggle queue-panel/lyrics-modal visibility via a class that triggers the new CSS transition instead of an instant display/visibility flip; update the now-playing bar's ambient backdrop element on track change.

## Edge Cases to Handle
- Browsers without View Transitions API (Firefox, older Safari): must fall back to the current instant swap, not a broken half-transition.
- `prefers-reduced-motion: reduce`: all motion reduced to near-instant, including the ambient backdrop crossfade.
- Rapid repeated navigation (double/triple hash-route clicks before a transition finishes): must not stack transitions or leave `#view` in an intermediate/broken state — guard against overlapping `startViewTransition` calls.
- Slow network: skeleton should not flash for near-instant loads and should not spin forever on a failed fetch — fall through to the existing error/empty state instead of an indefinite shimmer.
- Modal/panel animation must not break existing keyboard/focus behavior (ESC-to-close, click-outside-to-close) already in `player.js`.
- Ambient blurred backdrop must stay cheap (CSS filter on the art the app already loaded) so it doesn't regress performance on low-end devices playing the now-playing bar continuously.

## Test Scenarios
- Navigate Home → Search → Library → a detail view and back: transitions play smoothly, no blank flash, hash back/forward still works.
- Set OS-level "reduce motion" and repeat the above: transitions are suppressed/near-instant.
- Rapidly click three different nav items before a transition finishes: no stuck/blank view, final route matches the last click.
- Throttle network (Chrome DevTools "Slow 3G") loading a shelf/list: skeleton shows, replaced cleanly once data arrives; disconnect network entirely: existing error state shows, not an indefinite skeleton.
- Open/close the queue panel and lyrics modal several times in a row: animation plays consistently, ESC and click-outside still close it, focus isn't trapped.
- Play several tracks in sequence: now-playing bar's ambient backdrop crossfades to the new album art each time without stutter.
- Load the app in Firefox (no View Transitions API): navigation still works via the fallback path, no console errors.

## Scope

**Tier:** micro

**Reasoning:** No route/endpoint changes, no data-model changes, and no new UI surface — this polishes existing screens (motion, loading states, hover feedback) rather than adding one. Touches 3 existing files (`styles.css`, `app.js`, `player.js`) but each edit is additive CSS/small JS hooks into code paths that already exist. No specialist architect fits this stack anyway (VANILLA_WEB — no React/Express/Mongo), so fan-out would spawn nothing usable; implementing directly in-context is both the size-appropriate and the only workable choice here.

**Subagents to spawn:** none — implemented directly in main context per micro tier.

**Skills to invoke:** none required (`frontend-design` skill is for generating new components/pages from scratch; this is a motion/interaction pass on existing markup, so it's better done by editing the existing CSS/JS directly with full context of what's there). `simplify` skipped per micro tier. Product review (Step 4.5) skipped per micro tier.

## Model Plan

**Model Plan:** all sonnet (micro — no fan-out; also the standing project preference for maraetai — no escalation even where a rule would otherwise raise a step to opus)

## Open Questions
- View Transitions API + CSS fallback vs. hand-rolled CSS-class-toggle transitions everywhere: defaulting to the native API (zero new code paths to maintain, browser-native, degrades safely) per Search Before Building — flag if a stakeholder wants uniform behavior across all browsers today.
- How closely to mirror the native app's new ambient mesh-gradient motif vs. a web-native treatment: defaulting to a simplified (CSS-only, no video/mesh-gradient shader) echo of it for cross-platform consistency, since a full mesh-gradient/video treatment would need a canvas/WebGL layer this brief intentionally keeps out of scope.
