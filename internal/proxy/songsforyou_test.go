package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/unclefroob/maraetai-service/internal/store"
)

type songsForYouJSON struct {
	Response struct {
		Status string `json:"status"`
		Error  *struct {
			Code int `json:"code"`
		} `json:"error"`
		SongsForYou struct {
			Song []struct {
				ID     string `json:"id"`
				Artist string `json:"artist"`
				Reason string `json:"reason"`
			} `json:"song"`
		} `json:"songsForYou"`
	} `json:"subsonic-response"`
}

func fetchSongsForYou(t *testing.T, base, query string) songsForYouJSON {
	t.Helper()
	resp, err := http.Get(base + "/rest/getSongsForYou.view?" + query)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var r songsForYouJSON
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return r
}

func TestGetSongsForYouPersonalized(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, st := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	now := time.Now().UTC()
	// Two top artists → personalized path. Massive Attack via album al1, Portishead via al2.
	for i := 0; i < 4; i++ {
		_ = st.InsertPlay(context.Background(), store.Play{User: "alice", SongID: "p-ma" + string(rune('a'+i)), PlayedAt: now.AddDate(0, 0, -20), Artist: "Massive Attack", AlbumID: "al1"})
	}
	for i := 0; i < 2; i++ {
		_ = st.InsertPlay(context.Background(), store.Play{User: "alice", SongID: "p-po" + string(rune('a'+i)), PlayedAt: now.AddDate(0, 0, -20), Artist: "Portishead", AlbumID: "al2"})
	}

	r := fetchSongsForYou(t, srv.URL, "u=alice&t=good&s=salt&date=2026-06-22&count=10&f=json")
	if r.Response.Status != "ok" {
		t.Fatalf("status = %s", r.Response.Status)
	}
	songs := r.Response.SongsForYou.Song
	if len(songs) == 0 {
		t.Fatal("expected a non-empty mix")
	}

	// Every song carries a reason; we expect all three buckets represented.
	var sawRotation, sawDiscovery, sawFresh bool
	for _, s := range songs {
		if s.Reason == "" {
			t.Errorf("song %s missing reason", s.ID)
		}
		switch {
		case s.Artist == "Massive Attack" || s.Artist == "Portishead":
			sawRotation = true
		case s.Artist == "Tricky": // similar-to discovery
			sawDiscovery = true
		case s.ID == "rnd1" || s.ID == "rnd2":
			sawFresh = true
		}
	}
	if !sawRotation {
		t.Error("expected rotation (top-artist) tracks")
	}
	if !sawDiscovery {
		t.Error("expected discovery (similar-artist) tracks")
	}
	if !sawFresh {
		t.Error("expected a serendipity/random track")
	}
}

func TestGetSongsForYouDeterministicPerDay(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, st := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		_ = st.InsertPlay(context.Background(), store.Play{User: "alice", SongID: "x" + string(rune('a'+i)), PlayedAt: now.AddDate(0, 0, -10), Artist: "Massive Attack", AlbumID: "al1"})
		_ = st.InsertPlay(context.Background(), store.Play{User: "alice", SongID: "y" + string(rune('a'+i)), PlayedAt: now.AddDate(0, 0, -10), Artist: "Portishead", AlbumID: "al2"})
	}

	first := fetchSongsForYou(t, srv.URL, "u=alice&t=good&s=salt&date=2026-06-22&count=8&f=json")
	second := fetchSongsForYou(t, srv.URL, "u=alice&t=good&s=salt&date=2026-06-22&count=8&f=json")

	if len(first.Response.SongsForYou.Song) != len(second.Response.SongsForYou.Song) {
		t.Fatalf("length differs: %d vs %d", len(first.Response.SongsForYou.Song), len(second.Response.SongsForYou.Song))
	}
	for i := range first.Response.SongsForYou.Song {
		if first.Response.SongsForYou.Song[i].ID != second.Response.SongsForYou.Song[i].ID {
			t.Fatalf("same-day mix not stable at %d: %s vs %s", i,
				first.Response.SongsForYou.Song[i].ID, second.Response.SongsForYou.Song[i].ID)
		}
	}
}

func TestGetSongsForYouColdStart(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, _ := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// No play history → cold start: favourites + random.
	r := fetchSongsForYou(t, srv.URL, "u=newbie&t=good&s=salt&date=2026-06-22&count=10&f=json")
	if r.Response.Status != "ok" {
		t.Fatalf("status = %s", r.Response.Status)
	}
	var sawFav, sawRandom bool
	for _, s := range r.Response.SongsForYou.Song {
		if s.ID == "fav1" {
			sawFav = true
		}
		if s.ID == "rnd1" || s.ID == "rnd2" {
			sawRandom = true
		}
	}
	if !sawFav || !sawRandom {
		t.Errorf("cold start should blend favourites + random; sawFav=%v sawRandom=%v", sawFav, sawRandom)
	}
}

func TestGetSongsForYouRejectsBadCreds(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, _ := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	r := fetchSongsForYou(t, srv.URL, "u=alice&t=bad&s=salt&f=json")
	if r.Response.Status != "failed" || r.Response.Error == nil || r.Response.Error.Code != 40 {
		t.Errorf("expected wrong-credentials error, got %+v", r.Response)
	}
}
