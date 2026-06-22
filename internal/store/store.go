// Package store persists play history in a local SQLite database.
//
// It uses the pure-Go modernc.org/sqlite driver (no cgo) so the service keeps
// building as a single static binary.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Play is a single recorded listen, with a snapshot of the song's metadata as
// it was at scrobble time (so history renders even if the song is later
// edited or deleted upstream).
type Play struct {
	User     string
	SongID   string
	PlayedAt time.Time
	Client   string

	// Metadata snapshot (best-effort; may be empty if upstream lookup failed).
	Title    string
	Artist   string
	Album    string
	AlbumID  string
	CoverArt string
	Duration int

	// Plays is the play count for aggregate queries (e.g. On Repeat); 0 for
	// single-listen rows.
	Plays int
}

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path and applies
// migrations. Parent directories are created as required.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	// WAL + a busy timeout keep concurrent reads (SPA/read API) from colliding
	// with the write path under load.
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS plays (
    id         INTEGER PRIMARY KEY,
    user       TEXT    NOT NULL,
    song_id    TEXT    NOT NULL,
    played_at  INTEGER NOT NULL,           -- unix seconds
    client     TEXT    NOT NULL DEFAULT '',
    title      TEXT    NOT NULL DEFAULT '',
    artist     TEXT    NOT NULL DEFAULT '',
    album      TEXT    NOT NULL DEFAULT '',
    album_id   TEXT    NOT NULL DEFAULT '',
    cover_art  TEXT    NOT NULL DEFAULT '',
    duration   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_plays_user_time ON plays (user, played_at DESC);`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// InsertPlay records a single listen.
func (s *Store) InsertPlay(ctx context.Context, p Play) error {
	const q = `INSERT INTO plays
        (user, song_id, played_at, client, title, artist, album, album_id, cover_art, duration)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, q,
		p.User, p.SongID, p.PlayedAt.Unix(), p.Client,
		p.Title, p.Artist, p.Album, p.AlbumID, p.CoverArt, p.Duration,
	)
	if err != nil {
		return fmt.Errorf("insert play: %w", err)
	}
	return nil
}

// RecentlyPlayed returns a user's listens most-recent-first. (M2 will layer
// per-song de-duplication on top; this is the raw timeline.)
func (s *Store) RecentlyPlayed(ctx context.Context, user string, limit, offset int) ([]Play, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `SELECT song_id, played_at, client, title, artist, album, album_id, cover_art, duration
        FROM plays WHERE user = ? ORDER BY played_at DESC, id DESC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, q, user, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query recent: %w", err)
	}
	defer rows.Close()

	var out []Play
	for rows.Next() {
		var p Play
		var ts int64
		if err := rows.Scan(&p.SongID, &ts, &p.Client, &p.Title, &p.Artist,
			&p.Album, &p.AlbumID, &p.CoverArt, &p.Duration); err != nil {
			return nil, fmt.Errorf("scan recent: %w", err)
		}
		p.User = user
		p.PlayedAt = time.Unix(ts, 0).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

// RecentlyPlayedDistinct returns a user's listens collapsed to one row per
// song (the most recent play of each), most-recent-first. This is what the
// "recently played songs" view wants — no repeats when a track is looped.
//
// It relies on a documented SQLite feature: when a query uses MAX()/MIN() with
// bare (non-aggregated, non-grouped) columns, those columns take their values
// from the row that supplied the max/min. So MAX(played_at) per song_id yields
// the metadata snapshot from that song's most recent play.
func (s *Store) RecentlyPlayedDistinct(ctx context.Context, user string, limit, offset int) ([]Play, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `SELECT song_id, MAX(played_at) AS pa, client, title, artist, album, album_id, cover_art, duration
        FROM plays WHERE user = ?
        GROUP BY song_id
        ORDER BY pa DESC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, q, user, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query recent distinct: %w", err)
	}
	defer rows.Close()

	var out []Play
	for rows.Next() {
		var p Play
		var ts int64
		if err := rows.Scan(&p.SongID, &ts, &p.Client, &p.Title, &p.Artist,
			&p.Album, &p.AlbumID, &p.CoverArt, &p.Duration); err != nil {
			return nil, fmt.Errorf("scan recent distinct: %w", err)
		}
		p.User = user
		p.PlayedAt = time.Unix(ts, 0).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

// ArtistCount is a play tally for one artist.
type ArtistCount struct {
	Artist string `json:"artist"`
	Plays  int    `json:"plays"`
}

// SongCount is a play tally for one song.
type SongCount struct {
	SongID string `json:"songId"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Plays  int    `json:"plays"`
}

// DayCount is a play tally for one calendar day (UTC, YYYY-MM-DD).
type DayCount struct {
	Day   string `json:"day"`
	Plays int    `json:"plays"`
}

// Stats is a Wrapped-style summary of a user's listening since a given time.
type Stats struct {
	TotalPlays     int           `json:"totalPlays"`
	DistinctSongs  int           `json:"distinctSongs"`
	TotalDurationS int           `json:"totalDurationSeconds"`
	TopArtists     []ArtistCount `json:"topArtists"`
	TopSongs       []SongCount   `json:"topSongs"`
	PlaysByDay     []DayCount    `json:"playsByDay"`
}

// Stats aggregates a user's listening from `since` to now. topN bounds the
// top-artists and top-songs lists.
func (s *Store) Stats(ctx context.Context, user string, since time.Time, topN int) (*Stats, error) {
	if topN <= 0 {
		topN = 10
	}
	from := since.Unix()
	out := &Stats{
		TopArtists: []ArtistCount{},
		TopSongs:   []SongCount{},
		PlaysByDay: []DayCount{},
	}

	row := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COUNT(DISTINCT song_id), COALESCE(SUM(duration),0)
         FROM plays WHERE user = ? AND played_at >= ?`, user, from)
	if err := row.Scan(&out.TotalPlays, &out.DistinctSongs, &out.TotalDurationS); err != nil {
		return nil, fmt.Errorf("stats totals: %w", err)
	}

	if err := s.queryRows(ctx,
		`SELECT artist, COUNT(*) c FROM plays
         WHERE user = ? AND played_at >= ? AND artist <> ''
         GROUP BY artist ORDER BY c DESC, artist LIMIT ?`,
		[]any{user, from, topN},
		func(scan func(...any) error) error {
			var a ArtistCount
			if err := scan(&a.Artist, &a.Plays); err != nil {
				return err
			}
			out.TopArtists = append(out.TopArtists, a)
			return nil
		}); err != nil {
		return nil, fmt.Errorf("stats artists: %w", err)
	}

	if err := s.queryRows(ctx,
		`SELECT song_id, title, artist, COUNT(*) c FROM plays
         WHERE user = ? AND played_at >= ?
         GROUP BY song_id ORDER BY c DESC, title LIMIT ?`,
		[]any{user, from, topN},
		func(scan func(...any) error) error {
			var sc SongCount
			if err := scan(&sc.SongID, &sc.Title, &sc.Artist, &sc.Plays); err != nil {
				return err
			}
			out.TopSongs = append(out.TopSongs, sc)
			return nil
		}); err != nil {
		return nil, fmt.Errorf("stats songs: %w", err)
	}

	if err := s.queryRows(ctx,
		`SELECT date(played_at, 'unixepoch') d, COUNT(*) c FROM plays
         WHERE user = ? AND played_at >= ?
         GROUP BY d ORDER BY d`,
		[]any{user, from},
		func(scan func(...any) error) error {
			var d DayCount
			if err := scan(&d.Day, &d.Plays); err != nil {
				return err
			}
			out.PlaysByDay = append(out.PlaysByDay, d)
			return nil
		}); err != nil {
		return nil, fmt.Errorf("stats by-day: %w", err)
	}

	return out, nil
}

// queryRows runs a query and invokes fn for each row with its Scan function.
func (s *Store) queryRows(ctx context.Context, query string, args []any, fn func(scan func(...any) error) error) error {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows.Scan); err != nil {
			return err
		}
	}
	return rows.Err()
}

// OnRepeat returns the user's most-replayed songs since `since` — the songs
// genuinely on repeat, not just recently played. One row per song, play count
// in Play.Plays, ordered by play count then recency. `minPlays` filters out
// one-off listens; metadata is the snapshot from the most recent play (SQLite's
// MAX()+bare-column rule).
func (s *Store) OnRepeat(ctx context.Context, user string, since time.Time, minPlays, limit int) ([]Play, error) {
	if limit <= 0 {
		limit = 50
	}
	if minPlays <= 0 {
		minPlays = 1
	}
	const q = `SELECT song_id, MAX(played_at) AS pa, client, title, artist, album, album_id, cover_art, duration, COUNT(*) AS plays
        FROM plays WHERE user = ? AND played_at >= ?
        GROUP BY song_id
        HAVING plays >= ?
        ORDER BY plays DESC, pa DESC
        LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, user, since.Unix(), minPlays, limit)
	if err != nil {
		return nil, fmt.Errorf("query on-repeat: %w", err)
	}
	defer rows.Close()

	var out []Play
	for rows.Next() {
		var p Play
		var ts int64
		if err := rows.Scan(&p.SongID, &ts, &p.Client, &p.Title, &p.Artist,
			&p.Album, &p.AlbumID, &p.CoverArt, &p.Duration, &p.Plays); err != nil {
			return nil, fmt.Errorf("scan on-repeat: %w", err)
		}
		p.User = user
		p.PlayedAt = time.Unix(ts, 0).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

// ArtistAffinity is a user's play affinity for one artist, with a representative
// album id (for resolving the artist id upstream).
type ArtistAffinity struct {
	Artist     string
	Plays      int
	RepAlbumID string
}

// TopArtists returns the user's most-played artists since `since`, with a
// representative album id each — the affinity signal behind "Songs for you".
func (s *Store) TopArtists(ctx context.Context, user string, since time.Time, limit int) ([]ArtistAffinity, error) {
	if limit <= 0 {
		limit = 8
	}
	const q = `SELECT artist, COUNT(*) AS plays, MAX(album_id) AS rep
        FROM plays WHERE user = ? AND played_at >= ? AND artist <> ''
        GROUP BY artist ORDER BY plays DESC, artist LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, user, since.Unix(), limit)
	if err != nil {
		return nil, fmt.Errorf("query top artists: %w", err)
	}
	defer rows.Close()
	var out []ArtistAffinity
	for rows.Next() {
		var a ArtistAffinity
		if err := rows.Scan(&a.Artist, &a.Plays, &a.RepAlbumID); err != nil {
			return nil, fmt.Errorf("scan top artists: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RecentSongIDs returns the set of song ids the user played since `since` —
// used to exclude just-heard tracks from recommendations.
func (s *Store) RecentSongIDs(ctx context.Context, user string, since time.Time) (map[string]struct{}, error) {
	const q = `SELECT DISTINCT song_id FROM plays WHERE user = ? AND played_at >= ?`
	rows, err := s.db.QueryContext(ctx, q, user, since.Unix())
	if err != nil {
		return nil, fmt.Errorf("query recent ids: %w", err)
	}
	defer rows.Close()
	out := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan recent ids: %w", err)
		}
		out[id] = struct{}{}
	}
	return out, rows.Err()
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}
