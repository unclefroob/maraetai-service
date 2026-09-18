# Feature Brief: Layered Now-Playing Background (Ambient + Curated Video)

**Slug:** now-playing-ambient-video-background
**Date:** 2026-09-18
**Mode:** AUTOMATED
**Scope tier:** cross-repo

## Feature Request (verbatim, distilled from conversation)

Replace `FullPlayerView`'s current static 2-stop gradient background with a layered system:

1. **Ambient tier** (iOS + macOS): full-bleed blurred hi-res cover art + a slow animated gradient-mesh
   drift, derived from a new multi-color palette extraction (alongside the existing single-color
   `dominantColor(from:)`). Cross-fades on track change using the existing `.spotifySlow` curve, at the
   same point `extractDominantColor()` already fires. Falls back to today's static gradient when
   Reduce Motion is on, or when the "ambient background" Settings toggle is off, or when cover art is
   missing/failed (→ solid color).
2. **Video tier** (iPhone only — layered above ambient): a self-curated, muted, looping video clip
   played full-screen behind the now-playing UI, sourced from a new `maraetai-service` endpoint. Falls
   back to the ambient tier when no clip exists for the track/album, or when the "video background"
   Settings toggle (separate from the ambient toggle, framed around data usage) is off.
3. **Curation tooling**: a local `ffmpeg`-based script that cuts a fixed-portrait (1080×1920), silent,
   crossfaded loop from a full source video the user already has, and writes it to the served directory
   named by Subsonic ID. Not exposed over HTTP — an authoring tool run by hand.

**Full fallback chain:** video (if enabled + found) → ambient (if enabled) → today's static gradient
(Reduce Motion or ambient off) → solid color (art missing).

## Context

### maraetai (iOS/macOS SwiftUI client) — gathered via Explore agent

- **Background + lifecycle:** `Maraetai/Views/Player/FullPlayerView.swift` — `private var background`
  (lines 134–142) renders `LinearGradient([dominantColor, .black])`, animated via
  `.animation(.spotifySlow, value: dominantColor)`. `.onAppear` (57–67) and
  `.onChange(of: player.currentTrack?.id)` (68–71) call `extractDominantColor()` (434–456), which
  fetches cover art via Kingfisher and calls `ColorExtractor.dominantColor(from:)`. This is the
  insertion point for both new tiers.
- **Color extraction:** `Maraetai/Services/ColorExtractor.swift` — `dominantColor(from:)` (12–55),
  20×20 `CGContext` pixel sampling, single most-saturated pixel. macOS twin:
  `Maraetai Mac/Platform/MacImageColorExtractor.swift` — `extract(from:)` (36–97), explicitly documented
  as "must mirror iOS tuning." Platform-neutral protocol:
  `MaraetaiCore/Sources/MaraetaiCore/Platform/ImageColorExtracting.swift`
  (`dominantColor(from: Data) async -> RGBComponents?`, `RGBComponents` value type) — a new
  `palette(from:) async -> [RGBComponents]` requirement belongs here, implemented on both platforms.
  `Maraetai/Services/ColorExtractorConformance.swift` wires the iOS extractor to the core protocol —
  check when adding `palette()`.
- **Cover art + image loading:** `SubsonicAPIService.coverArtURL(id:size:quality:)`
  (`MaraetaiCore/.../SubsonicAPIService.swift`, 535–555), memoized because Kingfisher's `Source`
  identity is keyed on `downloadURL`. Kingfisher is the image library throughout.
- **Networking/URL building:** `SubsonicAPIService.buildURL(endpoint:extraParams:)` (91–99) is the
  single chokepoint. `streamURL`/`downloadURL`/`coverArtURL` (522–555, under
  `// MARK: - URL generators (for AVPlayer / Kingfisher)`) are the pattern for a new
  `getTrackVideoURL(id:)`. Existing comments (e.g. line 192) already flag proxy-only endpoints — same
  convention applies here.
- **Server type / proxy gating:** `MaraetaiCore/.../Network/MusicServer.swift` —
  `enum ServerType { case subsonic, maraetai }` (5–21); the maraetai-service adapter overrides
  `serverType` to `.maraetai` (183–187). The video endpoint only exists on the proxy, so video-tier
  availability must gate on `serverType == .maraetai` before even attempting the request. Exposed via
  `DependencyContainer`/`MacDependencyContainer` (`container.serverType`).
- **Track model:** `MaraetaiCore/.../Models/Domain/Track.swift` — has `id`, `coverArtId`, `albumId`; no
  video field. Availability is discovered purely by requesting the endpoint and handling 404 — no
  client-side metadata needed.
- **Settings:** `Maraetai/Views/Settings/SettingsRootView.swift` — sectioned list of sub-screens.
  Storage convention is **plain `@AppStorage` in the view**, no dedicated settings object for simple
  toggles — see `StreamingSettingsView.swift` (`@AppStorage("streamOriginalWifi")`,
  `@AppStorage("streamOriginalCellular")` — good precedent for a data-usage-gated toggle) and
  `VolumeNormalizationSettingsView.swift` (single `@AppStorage` + `Toggle`).
- **Video playback:** **none exists in the app today.** `AVFoundation`/`AVPlayer` is used only for audio
  (`Maraetai/Services/Audio/AudioPlayerService.swift`, macOS twin
  `Maraetai Mac/Audio/MacAudioPlayerService.swift`). No `AVQueuePlayer`, `AVPlayerLooper`,
  `AVPlayerLayer`, `VideoPlayer`/AVKit, `MeshGradient`, or `TimelineView` usage anywhere — all new.
- **Deployment target:** `IPHONEOS_DEPLOYMENT_TARGET = 26.2`, `MACOSX_DEPLOYMENT_TARGET = 26.5` — no
  availability guards needed for `MeshGradient`, `TimelineView`, or `AVPlayerLooper`.
- **Tests:** `MaraetaiTests/` mixes `XCTest` (`ArtistDetailViewModelTests.swift`,
  `ArtistDTOTests.swift`) and Swift Testing (`StringHTMLTests.swift`). `StubURLProtocol.swift`
  (`.makeSession()`, `.route([:])`, `.envelope(_:)`) is the pattern for stubbing
  `SubsonicAPIService` network calls in tests — use for `getTrackVideoURL`. No existing
  `ColorExtractor`/`MacImageColorExtractor` tests — `palette()` would be the first.
- No `CLAUDE.md` or `.autofeature/patterns.md` in this repo — no captured coding canon beyond reading
  source directly (done above). `PLAN.md`/`RELEASE.md` exist as process docs, not enforced canon.

### maraetai-service (Go reverse proxy) — read directly

- **Entry point:** `main.go` — builds `store.Open(cfg.DBPath)` and `proxy.New(upstream, st, ..., log)`.
- **Route registration pattern:** `internal/proxy/proxy.go` — every custom Subsonic-shaped endpoint is
  registered twice (`/rest/getX` and `/rest/getX.view`) on the mux, ahead of the catch-all reverse
  proxy. Existing examples: `getFavourites` (`internal/proxy/favourites.go`), `getArtistList`
  (`internal/proxy/artists.go`), `getOnRepeat` (`internal/proxy/onrepeat.go`) — each a small handler
  struct taking `auth.NewValidator(upstream)` (+ `navidrome.New(upstream)` when it needs upstream
  metadata, + `store` when it needs local persistence). `getTrackVideo` follows this exact shape.
- **Auth:** `internal/auth/validator.go` — `Validator.Validate(ctx, url.Values)` forwards `u`/`t`/`s`/`p`
  to upstream `ping.view`, no local user table, no admin/role concept at all. Every custom endpoint
  must call this before doing anything.
- **Store/persistence:** `internal/store/store.go` — pure-Go SQLite (`modernc.org/sqlite`), `migrate()`
  pattern (idempotent `CREATE TABLE IF NOT EXISTS` + per-column `ALTER TABLE` guarded by
  `hasColumn`). Not needed for this feature — video existence is discovered by filesystem `os.Stat`,
  not a DB row (no new table needed).
- **Config/storage location:** `internal/config/config.go` — `DB_PATH` env var, defaults to
  `./data/maraetai.db`. `docker-compose.yml` mounts `./data/maraetai-service:/data` into the proxy
  container, with `DB_PATH: /data/maraetai.db`. A new `VIDEOS_DIR` (default `./data/videos`, container
  default `/data/videos`) reuses the same already-mounted volume — no new Docker volume needed.
- **Navidrome metadata client:** `internal/navidrome/client.go` — used by `scrobbleTee`'s meta resolver
  and `songsForYou` to fetch song metadata (including `albumId`) from upstream. Reused for the
  track→album fallback resolution in `getTrackVideo`.
- **Security-relevant:** the `id` query param becomes part of a filesystem path
  (`<VIDEOS_DIR>/<id>.mp4`) — must be strictly validated (reject anything containing `/`, `..`, or
  non-Subsonic-ID characters) before touching the filesystem. This is a **path-traversal risk** to
  flag explicitly in the plan and pre-ship review.
- No dedicated Go architect agent exists in this AutoFeature plugin's roster (only
  `express-mongo-architect`/`react-architect`/`react-native-architect`/`swift-architect`/
  `kotlin-compose-architect`) — the orchestrator designs/implements this side directly against the
  conventions above, in lieu of a specialist agent.

## Cross-Repo Coordination

**Primary repo:** maraetai-service (backend, ships first — client tier gracefully falls back to
ambient on 404, but the endpoint should exist before the client starts calling it)
**Sibling repo:** maraetai (iOS/macOS client)
**Branch in both:** `feature/now-playing-ambient-video-background`
**Ship order:** maraetai-service → maraetai

(Sibling naming doesn't match the `-api`/`-mobile`/etc. suffix heuristic in
`cross-repo-detect.md` — the pairing is established directly from context: `maraetai-service` is the
Go backend for the `maraetai` client apps.)

## Scope

**Tier: cross-repo** — touches `maraetai-service` (new Go endpoint + curation script) and `maraetai`
(new SwiftUI views, color-extraction methods, networking method, Settings toggles, first video-playback
code in the app). Not `micro` (many files, two repos); not `single-layer`/`cross-stack` (spans repos,
not just layers within one).

## Model Plan

**Profile:** forced:sonnet — user override ("no, use sonnet"), superseding the E1 cross-repo
escalation rule that would otherwise raise Plan to opus. Every spawn in this run is sonnet.
**Rules fired:** none (user override pins the whole fleet to sonnet)

| Task | Model | Why |
|------|-------|-----|
| Plan | sonnet | user override |
| swift-architect (design) | sonnet | base |
| swift-architect (implement) | sonnet | base |
| Go backend design/implementation (orchestrator-direct, no dedicated agent) | sonnet | base |
| test-runner (both repos) | sonnet | base |
| review passes (critical / info / testing) | sonnet | base |
| everything else | sonnet | base |

**Escalations during run:** none yet.

## Implementation Plan

### Step 0: Scope Challenge

The four locked decisions hold up against the code, with one ambiguity surfaced and resolved below:

- **Fallback chain (video → ambient → static gradient → solid color):** consistent with the code.
  `FullPlayerView.background` today is exactly "static gradient → solid `ColorExtractor.fallback`";
  the new tiers slot in above it at the same `extractDominantColor()` call sites (`.onAppear` /
  `.onChange(of: player.currentTrack?.id)`). No contradiction.
- **Two independent Settings toggles:** consistent — `StreamingSettingsView.swift`'s
  `streamOriginalWifi`/`streamOriginalCellular` pair is a direct, already-proven precedent. No
  contradiction.
- **Video iPhone-only:** consistent with "first video code in the app" — no existing
  `UIDevice.current.userInterfaceIdiom` check anywhere, so this is new code, not a contradiction.
- **No DB table, filesystem-keyed-by-Subsonic-ID:** confirmed against `internal/store/store.go` —
  video existence is a pure `os.Stat`/`os.Open`, no migration needed.

**Ambiguity raised, resolved without a User Challenge:** the Plan pass (working only from this brief)
independently flagged that "ambient on iOS + macOS" has no macOS target to attach to — the Mac app's
now-playing surface is `Maraetai Mac/Views/Player/MacPlayerBar.swift`, a compact bottom bar, not a
full-screen player. This matches a decision already made earlier in the originating
`/autofeature:feature-review` run (before this brief was written): that review's `scope.fastFollow`
already listed *"macOS parity pass once the macOS now-playing surface is located"* — i.e. macOS was
never MVP for the ambient tier, only a fast-follow once its equivalent surface is identified. No scope
change needed; this build ships **iOS `FullPlayerView` only** for both the ambient and video tiers.
macOS ambient treatment (retrofitting `MacPlayerBar`'s `.ultraThinMaterial` background) is out of scope
for this run.

### Step 1: Architecture Review

#### maraetai-service (Go)

**New files:**
- `internal/proxy/trackvideo.go` — `trackVideoHandler` struct + `newTrackVideoHandler(auth *auth.Validator, nd *navidrome.Client, videosDir string, log *slog.Logger)`, mirroring `favourites.go`'s constructor shape.
- `internal/proxy/trackvideo_test.go` — reuses the existing `fakeNavidrome`/`teeProxy` helpers from `scrobble_test.go` (already serves `getSong.view` for id `"song123"` → `albumId: "alb9"`).
- `scripts/curate-canvas.sh` — new top-level `scripts/` dir (none exists yet). Not Go source, so it can't live under `internal/`; a root-level `scripts/` dir for auxiliary tooling is standard Go-repo convention.

**Handler shape** (registered in `proxy.go` alongside `getArtistSongs`/`getFavourites`/`getArtistList` — the "always offered, auth-only, no store needed" block, **not** inside `if st != nil`, since it needs no DB):

```go
func (h *trackVideoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    q := r.URL.Query()
    user, err := h.auth.Validate(r.Context(), q)
    ...
    id := q.Get("id")
    if !isValidVideoID(id) {
        h.log.Warn("trackVideo: rejected id", "reason", "invalid", "raw", id)
        http.Error(w, "invalid id", http.StatusBadRequest)
        return
    }
    if h.serve(w, r, id) { return }                       // song-level hit
    if albumID, ok := h.resolveAlbum(r.Context(), id, q); ok && isValidVideoID(albumID) {
        if h.serve(w, r, albumID) {                        // album-level fallback hit
            h.log.Info("trackVideo: hit", "tier", "album", "id", id, "albumId", albumID)
            return
        }
    }
    h.log.Info("trackVideo: miss", "id", id)
    http.NotFound(w, r)
}
```

**Important divergence from the mirrored pattern:** every sibling handler writes a Subsonic
XML/JSON envelope via `subsonic.WriteError`/`subsonic.Write*`, because their consumer is
`SubsonicAPIService.fetch()`. `getTrackVideo`'s consumer is `AVPlayer` loading a URL directly — no
envelope on success (raw video bytes) or failure. Auth failure, malformed id, and not-found should all
be **plain HTTP status codes** (401/400/404 via `http.Error`/`http.NotFound`), not
`subsonic.WriteError`. Reuse `h.auth.Validate(...)` for the auth *check* only.

**id-validation (concrete, security-critical — see judgment below):** anchor to an explicit
allowlist, not a denylist:

```go
var validVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
```

`regexp.MustCompile` is repo-precedent (`internal/proxy/search.go`'s `lrcTimestamp`). **Defense in
depth on top of the allowlist** — after `filepath.Join(videosDir, id+".mp4")`, verify the resolved
path still has `filepath.Clean(videosDir)` as a prefix before opening:

```go
full := filepath.Join(videosDir, id+".mp4")
if !strings.HasPrefix(full, filepath.Clean(videosDir)+string(os.PathSeparator)) {
    http.Error(w, "invalid id", http.StatusBadRequest)
    return
}
```

**Track→album fallback:** reuses `navidrome.Client.GetSong(ctx, id, authParams)` unchanged —
`Song.AlbumID` is already populated. No new navidrome.Client method needed. The auth-param-forwarding
loop (`u`/`t`/`s`/`p`/`c`/`v`) is duplicated in `favourites.go` and `songsforyou.go` already; factor a
shared `authParams(q url.Values) url.Values` helper rather than pasting a third copy.

**Config/deployment:**
- `internal/config/config.go`: add `VideosDir string`, env `VIDEOS_DIR`, default `./data/videos` (mirrors `DB_PATH`'s default pattern).
- `main.go`: after `store.Open`, call `os.MkdirAll(cfg.VideosDir, 0o755)` before `proxy.New(...)` — the distroless image has no shell.
- `proxy.New` signature grows a 5th param: `New(upstream *url.URL, st *store.Store, navidromePublicURL string, videosDir string, log *slog.Logger)`.
- `Dockerfile`: add `ENV VIDEOS_DIR=/data/videos` next to `ENV DB_PATH=/data/maraetai.db`.
- `docker-compose.yml`: add `VIDEOS_DIR: /data/videos` to the `proxy` service's `environment:` block. No new volume — `./data/maraetai-service:/data` already covers it.

**curate-canvas.sh:** ffmpeg-based, source video path + target Subsonic ID, cuts 1080×1920 portrait, `-an` (silent), crossfade-looped output, writes to `<VIDEOS_DIR>/<id>.mp4`. Validate its own id argument against the same character set as `validVideoID` in Go.

#### maraetai (iOS client — this pass is iOS-only, see Step 0)

**New files:**
- `Maraetai/Views/Player/AmbientBackgroundView.swift` — `MeshGradient`/`TimelineView` drift, palette + Reduce Motion/toggle-off static fallback.
- `Maraetai/Views/Player/VideoBackgroundView.swift` — iPhone-only `AVQueuePlayer` + `AVPlayerLooper`, wrapped via `UIViewRepresentable` around a raw `AVPlayerLayer` — **not** AVKit's `VideoPlayer` (shows its own transport controls, doesn't compose with a pre-built looping queue player the same way).

**Existing files, additions:**
- `MaraetaiCore/Sources/MaraetaiCore/Platform/ImageColorExtracting.swift` — add `func palette(from imageData: Data, count: Int) async -> [RGBComponents]` alongside `dominantColor(from:)`.
- `Maraetai/Services/ColorExtractor.swift`, `Maraetai/Services/ColorExtractorConformance.swift`, `Maraetai Mac/Platform/MacImageColorExtractor.swift` — implement `palette()` on all three, same 20×20 sampling tuning as `dominantColor`.
- `MaraetaiCore/.../SubsonicAPIService.swift` — new `getTrackVideoURL(id: String) throws -> URL`, under `// MARK: - URL generators (for AVPlayer / Kingfisher)`, via the same `buildURL(endpoint: "getTrackVideo", extraParams: [URLQueryItem(name: "id", value: id)])` chokepoint as `streamURL`/`downloadURL`/`coverArtURL`, doc-commented as maraetai-service-only.
- `Maraetai/Views/Player/FullPlayerView.swift` — `background` becomes a `ZStack` layering (bottom→top): static gradient/solid fallback → ambient tier (if enabled, Reduce Motion off, art present) → video tier (if enabled, iPhone, `.maraetai` server, video confirmed playable). Driven from the existing `.onAppear`/`.onChange(of: player.currentTrack?.id)`.
- `Maraetai/Views/Settings/SettingsRootView.swift` — new row in the `"General"` section → `NowPlayingBackgroundSettingsView` sub-screen housing both toggles (matching the "every root row is a NavigationLink" convention).
- Two `@AppStorage` toggles, plain-in-the-view: `@AppStorage("ambientBackgroundEnabled") = true`, `@AppStorage("videoBackgroundEnabled") = false` (default off — data-usage framing, same posture as `streamOriginalCellular` defaulting false).

**AVQueuePlayer / AVAudioSession:** `AudioSessionManager.configure()` sets category `.playback`;
`AudioPlayerService`/`AudioRouteObserver` listen session-wide to route-change/interruption
notifications. Set `AVPlayer.isMuted = true` defensively regardless of the clip's own audio track.
Real risk: `AVAudioSession` category renegotiation on `AVPlayer`/`AVQueuePlayer` instantiation possibly
interrupting the already-active audio playback — **cannot be verified by reading code alone; needs an
explicit device QA pass** (not simulator) confirming no duck/glitch when a clip starts, loops, or the
track changes mid-playback.

**Rapid track-skipping lifecycle:** a single owner must hold exactly one
`AVQueuePlayer`/`AVPlayerLooper`/`AVPlayerItem` trio at a time, tearing down the previous one
synchronously before building the next, gated by a per-track generation check. `extractDominantColor()`
today has no such guard against a stale async result landing after a subsequent track change — worth
fixing as part of this work (capture the track id before async work, discard on mismatch at
completion).

### Step 2: Code Quality Review

- **Go:** reuse `auth.NewValidator(upstream)` + `navidrome.New(upstream)` construction verbatim. Factor the `u`/`t`/`s`/`p`/`c`/`v` auth-param loop (currently duplicated in `favourites.go`/`songsforyou.go`) into a shared helper rather than a third copy.
- **Swift:** the 20×20 `CGContext` pixel-sampling loop is already duplicated in three places (`ColorExtractor.swift`, `ColorExtractorConformance.swift`, `MacImageColorExtractor.swift`). Implement `palette()` by keeping a top-N-by-saturation list during the **same single pass** `dominantColor` already does (`dominantColor(from:) == palette(from:count:1).first`) — one `CGContext` render + one pixel loop per image per platform, not two.
- Reuse `buildURL(endpoint:extraParams:)` for `getTrackVideoURL` — don't bypass the chokepoint/memoization pattern `coverArtURL` established.

### Step 3: Unit Test Plan

**Go — `internal/proxy/trackvideo_test.go`:**
- Happy — found via song id: file at `<tmpVideosDir>/song123.mp4`, request with `id=song123`, assert 200 + correct bytes + Content-Type.
- Happy — found via album fallback: no `song123.mp4`, `alb9.mp4` exists; assert 200 + bytes from album file, "tier=album" log line.
- Not found: neither file exists; assert 404.
- Malformed id: `/`, `..`, `\`, empty, `.`, overlong (>128 chars), adversarial `../../etc/passwd` and pre-decoded `%2e%2e%2f`; assert 400, and assert no `os.Open` ever reaches outside `videosDir` (sandboxed `t.TempDir()` with a sentinel file one directory above that must never be readable through the endpoint).
- Unauthenticated: bad/missing creds → 401 (plain status, not Subsonic XML).
- Range requests: `Range: bytes=0-99` → 206 with correct slice.

**Swift:**
- `palette()` — happy (multi-color image → N distinct RGBComponents), blank/solid-color image (graceful single-ish result, no crash), grayscale (degrades like `dominantColor`'s existing `foundCandidate == false` path).
- `getTrackVideoURL(id:)` — synchronous URL-generator test (first test of the whole `// MARK: - URL generators` section — none exist today): path is `rest/getTrackVideo`, `id` query item present, auth query items present.
- Fallback-chain in the background view: video-off (ambient shown) / ambient-off + video-on (video shown alone) / both off (static gradient) / video 404s (ambient stays visible, no black frame) / Reduce Motion on (forces static gradient regardless of toggle state).

### Step 3b: Error & Rescue Map

- **Network failure fetching the video:** `AVPlayerItem` never reaches `.readyToPlay` (`.failed`/timeout) → treat like 404: ambient tier stays visible, video layer never reveals, no retry loop within the same track's session.
- **Corrupt/unplayable file on disk:** server opens/serves fine (file exists), client `AVPlayerItem` surfaces `.failed` — same client-side handling as network failure. Server can't validate video decodability without decoding it — accepted gap, relies on the curation script producing valid output.
- **AVPlayer failing to load:** observe `AVPlayerItem.status` transition to `.readyToPlay`/`.failed` before revealing the video layer — never reveal on bare "player created."

### Step 3c: Shadow Path Testing

1. Go: song-id hit / album-fallback hit / miss / malformed id (traversal + oversized + empty) / unauthenticated / Range request.
2. Swift `palette()`: multi-color / solid-color / grayscale / undecodable data.
3. Swift `getTrackVideoURL`: normal id / not-configured edge (mirrors `buildURL`'s existing throw path).
4. Background view composition: video+ambient both on+found (video wins) / video on+not found (ambient shows) / video off+ambient on (ambient shows) / both off (static gradient) / Reduce Motion on regardless (static gradient) / art missing entirely (solid color, existing behavior preserved) / rapid track skip mid-video-load (no leaked player, final track wins).

### Step 3d: Observability Checklist

Server-side, extending the existing `slog` pattern (`logging()` middleware already captures method/path/status/bytes generically):
- `trackVideo: hit` — fields: `tier` (song|album), `id`, `albumId` (when tier=album).
- `trackVideo: miss` — field: `id`.
- `trackVideo: rejected id` — fields: `reason=invalid`, `raw` id (own distinct line — a burst could indicate a client bug or active probing, separate signal from an ordinary 404).
- Auth failures: match `onrepeat.go`'s existing asymmetry — log unreachable-upstream failures, don't bother logging routine wrong-credentials rejections (already visible in the generic access log's 401 status).

### Step 4: Performance Review

- **Video file serving:** `http.ServeContent` handles Range/`ETag`/partial content — no custom logic needed.
- **AVPlayer memory/lifecycle on rapid track-skipping:** single owner holds one `AVQueuePlayer`/`AVPlayerLooper`/`AVPlayerItem` trio at a time, torn down synchronously before the next, gated by the same per-track generation check as `extractDominantColor()`'s race fix. Without this, a skip spree could spin up multiple players' worth of decode/buffering before settling.
- **TimelineView cost when video is also active — recommend PAUSE, not keep-running:** the video tier is full-bleed and fully occludes the ambient tier once confirmed playing (per the layering: video sits above ambient). A continuously animated `MeshGradient` underneath a fully opaque video is wasted GPU/CPU at the exact moment the device is already paying for video decode + live audio decode. Recommendation: pause (freeze on last frame, don't tear down/reset) the `TimelineView` schedule while the video tier is visible, so it's instantly resumable — the moment the video 404s mid-session, the toggle is disabled, or the *next* clip is still loading and the ambient tier needs to be the visible layer underneath. Caveat: this assumes the video layer is genuinely full-bleed opaque as described; if a future iteration makes it translucent/non-full-bleed, the recommendation flips to "keep running."

### Security judgment: id-validation / path-traversal

Treated as **genuine security-critical**, warranting extra pre-ship scrutiny, despite not matching a
literal E2 category (auth/payments/etc.). This proxy has **no local user/role table at all** — every
authenticated Navidrome user is trusted equally — and untrusted request input is interpolated directly
into a filesystem path that is opened and streamed back over HTTP. A bug in the allowlist/prefix-check
pair (unanchored regex, a `filepath.Join` edge case, an encoding path `net/url` doesn't normalize the
way assumed) has a blast radius of arbitrary file read on the container — and since `DB_PATH`
(`/data/maraetai.db`) and `VIDEOS_DIR` (`/data/videos`) share one mounted volume, a successful
traversal could plausibly reach the play-history database itself. The pre-ship reviewer must
specifically confirm: the regex is `^...$`-anchored (not bare `MatchString`), the defense-in-depth
prefix check exists and is tested, and the adversarial test cases in Step 3/3c actually pass.

### Critical Files
- /home/ryan/dev/maraetai-service/internal/proxy/proxy.go
- /home/ryan/dev/maraetai-service/internal/proxy/onrepeat.go
- /home/ryan/dev/maraetai-service/internal/navidrome/client.go
- /home/ryan/dev/maraetai-service/internal/config/config.go
- /home/ryan/dev/maraetai/Maraetai/Views/Player/FullPlayerView.swift
- /home/ryan/dev/maraetai/Maraetai/Services/ColorExtractor.swift
- /home/ryan/dev/maraetai/MaraetaiCore/Sources/MaraetaiCore/Services/Network/SubsonicAPIService.swift
- /home/ryan/dev/maraetai/Maraetai/Services/Audio/AudioSessionManager.swift

## Adaptations from standard AutoFeature methodology (noted for transparency)

- `TaskCreate`/`TaskUpdate` tools are unavailable in this environment — progress is tracked narratively
  instead of via the pipeline task list.
- Step 4.5 (pre-build product review Workflow) is **skipped** — the product shape (MVP scope, tier
  ordering, fallback chain, two independent toggles, phone-only video) was already decided
  interactively with the user across a `/autofeature:feature-review` run and follow-up design
  discussion in this same session; re-running an automated product panel over decisions the user
  already made explicitly would not surface new signal.
- No Go backend architect agent exists — the orchestrator performs that design/implementation role
  directly (see maraetai-service Context above), rather than via a specialist subagent.
