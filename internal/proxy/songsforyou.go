package proxy

import (
	"context"
	"errors"
	"hash/fnv"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/unclefroob/maraetai-service/internal/auth"
	"github.com/unclefroob/maraetai-service/internal/navidrome"
	"github.com/unclefroob/maraetai-service/internal/store"
	"github.com/unclefroob/maraetai-service/internal/subsonic"
)

// Songs-for-you tuning (all server-side; balanced 50/30/20 mix).
const (
	sfyAffinityDays      = 90 // window for "your top artists"
	sfyExcludeDays       = 14 // drop tracks heard this recently
	sfyTopArtists        = 8
	sfyDiscoverySeeds    = 3 // top artists used to find similar artists
	sfySimilarPerSeed    = 2
	sfyTopSongsPerArtist = 10
	sfyTopSongsPerSim    = 5
	sfyMaxPerArtist      = 2
	sfyMinArtists        = 2 // below this → cold start
	sfyConcurrency       = 8
	sfyDefaultCount      = 20
	sfyMaxCount          = 100
	sfyWeightA           = 50 // your rotation
	sfyWeightB           = 30 // discovery (similar artists)
)

// songsForYouHandler serves /rest/getSongsForYou: a personalized daily mix built
// from the user's play history (which plain Navidrome can't do). Blends tracks
// from the user's top artists, discovery via similar artists, and serendipity;
// falls back to favourites + random when history is thin.
type songsForYouHandler struct {
	store *store.Store
	auth  *auth.Validator
	nd    *navidrome.Client
	log   *slog.Logger

	mu    sync.Mutex
	cache map[string][]subsonic.Child // key: user|date|count → mix (daily)
}

func newSongsForYouHandler(st *store.Store, validator *auth.Validator, nd *navidrome.Client, log *slog.Logger) *songsForYouHandler {
	return &songsForYouHandler{store: st, auth: validator, nd: nd, log: log, cache: map[string][]subsonic.Child{}}
}

func (h *songsForYouHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	user, err := h.auth.Validate(r.Context(), q)
	switch {
	case errors.Is(err, auth.ErrMissingParams):
		subsonic.WriteError(w, q, subsonic.ErrRequiredParam, "Required parameter is missing")
		return
	case errors.Is(err, auth.ErrUnauthorized):
		subsonic.WriteError(w, q, subsonic.ErrWrongCredentials, "Wrong username or password")
		return
	case err != nil:
		h.log.Error("songsForYou: auth validation failed", "err", err)
		subsonic.WriteError(w, q, subsonic.ErrGeneric, "Authentication unavailable")
		return
	}

	count := clampInt(q.Get("count"), sfyDefaultCount, 1, sfyMaxCount)
	date := q.Get("date")
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}

	// Auth params reused for upstream content calls.
	authParams := url.Values{}
	for _, k := range []string{"u", "t", "s", "p", "c", "v"} {
		if v := q.Get(k); v != "" {
			authParams.Set(k, v)
		}
	}

	key := user + "|" + date + "|" + strconv.Itoa(count)
	h.mu.Lock()
	cached, ok := h.cache[key]
	h.mu.Unlock()
	if ok {
		subsonic.WriteSongsForYou(w, q, cached)
		return
	}

	seed := int64(hashSeed(user + "|" + date))
	mix := h.build(r.Context(), user, authParams, count, seed)

	h.mu.Lock()
	// Bound cache growth: a new day's key replaces, but cap total entries.
	if len(h.cache) > 256 {
		h.cache = map[string][]subsonic.Child{}
	}
	h.cache[key] = mix
	h.mu.Unlock()

	subsonic.WriteSongsForYou(w, q, mix)
}

func (h *songsForYouHandler) build(ctx context.Context, user string, authParams url.Values, count int, seed int64) []subsonic.Child {
	now := time.Now()
	topArtists, err := h.store.TopArtists(ctx, user, now.AddDate(0, 0, -sfyAffinityDays), sfyTopArtists)
	if err != nil {
		h.log.Warn("songsForYou: top artists failed", "user", user, "err", err)
	}
	recent, err := h.store.RecentSongIDs(ctx, user, now.AddDate(0, 0, -sfyExcludeDays))
	if err != nil {
		recent = map[string]struct{}{}
	}
	rng := rand.New(rand.NewSource(seed))

	if len(topArtists) < sfyMinArtists {
		return h.coldStart(ctx, authParams, recent, count, rng)
	}

	poolA := h.fromYourArtists(ctx, authParams, topArtists)
	poolB := h.discovery(ctx, authParams, topArtists)
	poolC := h.serendipity(ctx, authParams, count)

	return assembleMix(poolA, poolB, poolC, count, recent, rng)
}

// fromYourArtists (bucket A): top songs by each of the user's top artists,
// fetched with bounded concurrency.
func (h *songsForYouHandler) fromYourArtists(ctx context.Context, authParams url.Values, artists []store.ArtistAffinity) []subsonic.Child {
	perArtist := make([][]subsonic.Child, len(artists))
	sem := make(chan struct{}, sfyConcurrency)
	var wg sync.WaitGroup
	for i, a := range artists {
		wg.Add(1)
		sem <- struct{}{}
		go func(slot int, name string) {
			defer wg.Done()
			defer func() { <-sem }()
			songs, err := h.nd.GetTopSongs(ctx, name, sfyTopSongsPerArtist, authParams)
			if err != nil {
				h.log.Warn("songsForYou: getTopSongs failed", "artist", name, "err", err)
				return
			}
			perArtist[slot] = toChildren(songs, "Because you play "+name)
		}(i, a.Artist)
	}
	wg.Wait()
	var out []subsonic.Child
	for _, c := range perArtist {
		out = append(out, c...)
	}
	return out
}

// discovery (bucket B): songs by artists similar to the user's top artists,
// excluding artists they already play. Tolerant of getArtistInfo2 failures.
func (h *songsForYouHandler) discovery(ctx context.Context, authParams url.Values, artists []store.ArtistAffinity) []subsonic.Child {
	known := map[string]struct{}{}
	for _, a := range artists {
		known[a.Artist] = struct{}{}
	}
	var out []subsonic.Child
	seeds := artists
	if len(seeds) > sfyDiscoverySeeds {
		seeds = seeds[:sfyDiscoverySeeds]
	}
	for _, seed := range seeds {
		if seed.RepAlbumID == "" {
			continue
		}
		artistID, err := h.nd.GetAlbumArtistID(ctx, seed.RepAlbumID, authParams)
		if err != nil || artistID == "" {
			continue
		}
		similar, err := h.nd.GetSimilarArtists(ctx, artistID, sfySimilarPerSeed*3, authParams)
		if err != nil {
			continue
		}
		picked := 0
		for _, sim := range similar {
			if picked >= sfySimilarPerSeed {
				break
			}
			if _, isKnown := known[sim.Name]; isKnown || sim.Name == "" {
				continue
			}
			songs, err := h.nd.GetTopSongs(ctx, sim.Name, sfyTopSongsPerSim, authParams)
			if err != nil || len(songs) == 0 {
				continue
			}
			out = append(out, toChildren(songs, "Similar to "+seed.Artist)...)
			known[sim.Name] = struct{}{} // don't seed the same similar artist twice
			picked++
		}
	}
	return out
}

// serendipity (bucket C): random library songs.
func (h *songsForYouHandler) serendipity(ctx context.Context, authParams url.Values, count int) []subsonic.Child {
	n := count
	if n < sfyDefaultCount {
		n = sfyDefaultCount
	}
	songs, err := h.nd.GetRandomSongs(ctx, n, authParams)
	if err != nil {
		h.log.Warn("songsForYou: getRandomSongs failed", "err", err)
		return nil
	}
	return toChildren(songs, "Fresh pick")
}

// coldStart: thin history → favourites + random, daily-seeded.
func (h *songsForYouHandler) coldStart(ctx context.Context, authParams url.Values, recent map[string]struct{}, count int, rng *rand.Rand) []subsonic.Child {
	var pool []subsonic.Child
	if starred, err := h.nd.GetStarredSongs(ctx, authParams); err == nil {
		pool = append(pool, toChildren(starred, "From your favourites")...)
	}
	pool = append(pool, h.serendipity(ctx, authParams, count)...)
	return assembleMix(pool, nil, nil, count, recent, rng)
}

// assembleMix filters exclusions, dedupes, caps per artist, fills the 50/30/20
// targets (backfilling shortfalls), then daily-seed-shuffles the final order.
func assembleMix(poolA, poolB, poolC []subsonic.Child, count int, recent map[string]struct{}, rng *rand.Rand) []subsonic.Child {
	shuffle(poolA, rng)
	shuffle(poolB, rng)
	shuffle(poolC, rng)

	targetA := count * sfyWeightA / 100
	targetB := count * sfyWeightB / 100
	targetC := count - targetA - targetB

	seen := map[string]struct{}{}
	perArtist := map[string]int{}
	var result []subsonic.Child

	add := func(pool []subsonic.Child, target int) {
		taken := 0
		for _, c := range pool {
			if taken >= target || len(result) >= count {
				break
			}
			if !accept(c, recent, seen, perArtist) {
				continue
			}
			result = append(result, c)
			taken++
		}
	}
	add(poolA, targetA)
	add(poolB, targetB)
	add(poolC, targetC)

	// Backfill to `count` from whatever's left across all pools.
	for _, pool := range [][]subsonic.Child{poolA, poolB, poolC} {
		for _, c := range pool {
			if len(result) >= count {
				break
			}
			if accept(c, recent, seen, perArtist) {
				result = append(result, c)
			}
		}
	}

	shuffle(result, rng)
	return result
}

// accept returns true (and records the pick) if a song passes exclusion, global
// dedupe, and the per-artist cap.
func accept(c subsonic.Child, recent, seen map[string]struct{}, perArtist map[string]int) bool {
	if c.ID == "" {
		return false
	}
	if _, played := recent[c.ID]; played {
		return false
	}
	if _, dup := seen[c.ID]; dup {
		return false
	}
	if perArtist[c.Artist] >= sfyMaxPerArtist {
		return false
	}
	seen[c.ID] = struct{}{}
	perArtist[c.Artist]++
	return true
}

func toChildren(songs []navidrome.Song, reason string) []subsonic.Child {
	out := make([]subsonic.Child, 0, len(songs))
	for _, s := range songs {
		out = append(out, subsonic.Child{
			ID:          s.ID,
			IsDir:       false,
			Title:       s.Title,
			Album:       s.Album,
			Artist:      s.Artist,
			AlbumID:     s.AlbumID,
			CoverArt:    s.CoverArt,
			Duration:    s.Duration,
			Suffix:      s.Suffix,
			ContentType: s.ContentType,
			BitRate:     s.BitRate,
			Type:        "music",
			Reason:      reason,
		})
	}
	return out
}

func shuffle(s []subsonic.Child, rng *rand.Rand) {
	rng.Shuffle(len(s), func(i, j int) { s[i], s[j] = s[j], s[i] })
}

func hashSeed(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}
