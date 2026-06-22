package proxy

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/unclefroob/maraetai-service/internal/store"
)

// fakeNavidrome serves getSong metadata and records scrobble forwards.
func fakeNavidrome(t *testing.T, scrobbleHits *int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/getSong.view", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") != "song123" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"subsonic-response":{"status":"ok","song":{
			"id":"song123","title":"Teardrop","artist":"Massive Attack",
			"album":"Mezzanine","albumId":"alb9","coverArt":"art9","duration":331}}}`)
	})
	scrobble := func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(scrobbleHits, 1)
		_, _ = io.WriteString(w, `{"subsonic-response":{"status":"ok"}}`)
	}
	mux.HandleFunc("/rest/scrobble", scrobble)
	mux.HandleFunc("/rest/scrobble.view", scrobble)
	// getArtist returns two album stubs; getAlbum returns that album's songs.
	mux.HandleFunc("/rest/getArtist.view", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"subsonic-response":{"status":"ok","artist":{"id":"art1","name":"Massive Attack","album":[{"id":"al1"},{"id":"al2"}]}}}`)
	})
	mux.HandleFunc("/rest/getAlbum.view", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("id") {
		case "al1":
			_, _ = io.WriteString(w, `{"subsonic-response":{"status":"ok","album":{"id":"al1","song":[
				{"id":"s1","title":"Angel","artist":"Massive Attack","album":"Mezzanine","albumId":"al1","duration":379},
				{"id":"s2","title":"Teardrop","artist":"Massive Attack","album":"Mezzanine","albumId":"al1","duration":331}]}}}`)
		case "al2":
			_, _ = io.WriteString(w, `{"subsonic-response":{"status":"ok","album":{"id":"al2","song":[
				{"id":"s3","title":"Protection","artist":"Massive Attack","album":"Protection","albumId":"al2","duration":468}]}}}`)
		default:
			_, _ = io.WriteString(w, `{"subsonic-response":{"status":"failed","error":{"code":70,"message":"not found"}}}`)
		}
	})
	// ping for forward-and-validate auth: bad token => failed status (200).
	mux.HandleFunc("/rest/ping.view", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("t") == "bad" {
			_, _ = io.WriteString(w, `{"subsonic-response":{"status":"failed","error":{"code":40,"message":"Wrong username or password"}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"subsonic-response":{"status":"ok"}}`)
	})
	return httptest.NewServer(mux)
}

func teeProxy(t *testing.T, upstreamURL string) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	u, _ := url.Parse(upstreamURL)
	return New(u, st, slog.New(slog.NewTextHandler(io.Discard, nil))), st
}

// waitForPlays polls until the async recorder has written n plays, or fails.
func waitForPlays(t *testing.T, st *store.Store, user string, n int) []store.Play {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		plays, err := st.RecentlyPlayed(context.Background(), user, 50, 0)
		if err != nil {
			t.Fatalf("recent: %v", err)
		}
		if len(plays) >= n {
			return plays
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d plays", n)
	return nil
}

func TestScrobbleRecordsPlayAndForwards(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, st := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/rest/scrobble.view?u=alice&c=ios&id=song123&time=1700000000000")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()

	// Forwarded upstream unchanged.
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("scrobble forwarded %d times, want 1", hits)
	}

	plays := waitForPlays(t, st, "alice", 1)
	p := plays[0]
	if p.SongID != "song123" || p.Client != "ios" {
		t.Errorf("play = %+v", p)
	}
	if p.Title != "Teardrop" || p.Artist != "Massive Attack" || p.Album != "Mezzanine" {
		t.Errorf("metadata not snapshotted: %+v", p)
	}
	if p.Duration != 331 || p.AlbumID != "alb9" {
		t.Errorf("metadata fields wrong: %+v", p)
	}
	if want := time.UnixMilli(1700000000000).UTC(); !p.PlayedAt.Equal(want) {
		t.Errorf("playedAt = %v, want %v", p.PlayedAt, want)
	}
}

func TestNowPlayingNotificationNotRecorded(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, st := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// submission=false is a now-playing ping, not a completed play.
	resp, err := http.Get(srv.URL + "/rest/scrobble.view?u=bob&c=ios&id=song123&submission=false")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()

	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("still forwarded? hits=%d", hits)
	}
	// Give any erroneous async write a chance to land, then assert none did.
	time.Sleep(200 * time.Millisecond)
	plays, err := st.RecentlyPlayed(context.Background(), "bob", 50, 0)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(plays) != 0 {
		t.Errorf("now-playing ping was recorded: %+v", plays)
	}
}

func TestScrobbleWithMissingMetadataStillRecords(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, st := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Unknown id → getSong 404s, but the play should still be recorded (bare).
	resp, err := http.Get(srv.URL + "/rest/scrobble.view?u=carol&c=mac&id=unknown")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()

	plays := waitForPlays(t, st, "carol", 1)
	if plays[0].SongID != "unknown" || plays[0].Title != "" {
		t.Errorf("expected bare play, got %+v", plays[0])
	}
}
