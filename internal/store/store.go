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

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}
