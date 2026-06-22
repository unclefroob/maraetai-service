package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestRecentlyPlayedDistinctDedupsAndOrders(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	base := time.Unix(1700000000, 0).UTC()

	mustInsert := func(song string, at time.Time, title string) {
		if err := st.InsertPlay(ctx, Play{
			User: "alice", SongID: song, PlayedAt: at, Client: "ios",
			Title: title, Artist: "A", Album: "Alb", Duration: 100,
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// song1 played twice; song2 once in between.
	mustInsert("song1", base, "old-title")
	mustInsert("song2", base.Add(1*time.Minute), "two")
	mustInsert("song1", base.Add(2*time.Minute), "new-title")

	got, err := st.RecentlyPlayedDistinct(ctx, "alice", 10, 0)
	if err != nil {
		t.Fatalf("distinct: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 distinct songs, got %d: %+v", len(got), got)
	}
	// Most recent first: song1 (latest play) then song2.
	if got[0].SongID != "song1" || got[1].SongID != "song2" {
		t.Errorf("order = %s, %s", got[0].SongID, got[1].SongID)
	}
	// Metadata comes from the most recent play of song1.
	if got[0].Title != "new-title" {
		t.Errorf("snapshot from wrong row: %q", got[0].Title)
	}
	if !got[0].PlayedAt.Equal(base.Add(2 * time.Minute)) {
		t.Errorf("playedAt = %v", got[0].PlayedAt)
	}
}

func TestStatsAggregates(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	play := func(song, title, artist string, dur int, at time.Time) {
		if err := st.InsertPlay(ctx, Play{
			User: "alice", SongID: song, PlayedAt: at, Client: "ios",
			Title: title, Artist: artist, Album: "Alb", Duration: dur,
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// alice: song1 x3 (Massive Attack), song2 x1 (Portishead), song3 x2 (Massive Attack)
	for i := 0; i < 3; i++ {
		play("song1", "Angel", "Massive Attack", 379, now.Add(-time.Duration(i)*time.Hour))
	}
	play("song2", "Roads", "Portishead", 300, now.Add(-2*time.Hour))
	for i := 0; i < 2; i++ {
		play("song3", "Teardrop", "Massive Attack", 331, now.Add(-time.Duration(i)*time.Minute))
	}
	// a different user's plays must not leak in.
	play2 := Play{User: "bob", SongID: "x", PlayedAt: now, Title: "X", Artist: "Y", Duration: 100}
	if err := st.InsertPlay(ctx, play2); err != nil {
		t.Fatal(err)
	}

	s, err := st.Stats(ctx, "alice", now.Add(-24*time.Hour), 10)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if s.TotalPlays != 6 {
		t.Errorf("totalPlays = %d, want 6", s.TotalPlays)
	}
	if s.DistinctSongs != 3 {
		t.Errorf("distinctSongs = %d, want 3", s.DistinctSongs)
	}
	if want := 3*379 + 300 + 2*331; s.TotalDurationS != want {
		t.Errorf("totalDuration = %d, want %d", s.TotalDurationS, want)
	}
	// Massive Attack (5 plays) ahead of Portishead (1).
	if len(s.TopArtists) != 2 || s.TopArtists[0].Artist != "Massive Attack" || s.TopArtists[0].Plays != 5 {
		t.Errorf("topArtists = %+v", s.TopArtists)
	}
	// song1 (3 plays) is the top song.
	if len(s.TopSongs) == 0 || s.TopSongs[0].SongID != "song1" || s.TopSongs[0].Plays != 3 {
		t.Errorf("topSongs = %+v", s.TopSongs)
	}
	if len(s.PlaysByDay) == 0 {
		t.Errorf("playsByDay empty")
	}
}

func TestRecentlyPlayedDistinctScopedByUser(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	_ = st.InsertPlay(ctx, Play{User: "alice", SongID: "s1", PlayedAt: now})
	_ = st.InsertPlay(ctx, Play{User: "bob", SongID: "s2", PlayedAt: now})

	got, err := st.RecentlyPlayedDistinct(ctx, "alice", 10, 0)
	if err != nil {
		t.Fatalf("distinct: %v", err)
	}
	if len(got) != 1 || got[0].SongID != "s1" {
		t.Errorf("user scoping leaked: %+v", got)
	}
}

func TestOnRepeat(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	play := func(song string, at time.Time) {
		if err := st.InsertPlay(ctx, Play{User: "alice", SongID: song, PlayedAt: at, Title: song}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	// A: 4 recent plays (on repeat); C: 3 recent (on repeat); B: 2 recent (below
	// threshold); D: 5 plays but all >30 days ago (outside window).
	for i := 0; i < 4; i++ {
		play("A", now.Add(-time.Duration(i)*time.Hour))
	}
	for i := 0; i < 3; i++ {
		play("C", now.Add(-time.Duration(i)*time.Hour))
	}
	for i := 0; i < 2; i++ {
		play("B", now.Add(-time.Duration(i)*time.Hour))
	}
	for i := 0; i < 5; i++ {
		play("D", now.AddDate(0, 0, -40).Add(-time.Duration(i)*time.Hour))
	}

	got, err := st.OnRepeat(ctx, "alice", now.AddDate(0, 0, -30), 3, 50)
	if err != nil {
		t.Fatalf("onRepeat: %v", err)
	}
	// Only A and C qualify; A (4 plays) before C (3 plays).
	if len(got) != 2 {
		t.Fatalf("want 2 on-repeat songs, got %d: %+v", len(got), got)
	}
	if got[0].SongID != "A" || got[0].Plays != 4 {
		t.Errorf("first = %s (%d plays), want A (4)", got[0].SongID, got[0].Plays)
	}
	if got[1].SongID != "C" || got[1].Plays != 3 {
		t.Errorf("second = %s (%d plays), want C (3)", got[1].SongID, got[1].Plays)
	}
}
