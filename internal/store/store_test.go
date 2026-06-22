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
