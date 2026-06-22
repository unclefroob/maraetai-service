package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/unclefroob/maraetai-service/internal/store"
)

type recentsJSON struct {
	Response struct {
		Status string `json:"status"`
		Error  *struct {
			Code int `json:"code"`
		} `json:"error"`
		RecentlyPlayed struct {
			Song []struct {
				ID       string `json:"id"`
				Title    string `json:"title"`
				PlayedAt int64  `json:"playedAt"`
			} `json:"song"`
		} `json:"recentlyPlayed"`
	} `json:"subsonic-response"`
}

func seedPlay(t *testing.T, st *store.Store, user, song, title string, at time.Time) {
	t.Helper()
	if err := st.InsertPlay(context.Background(), store.Play{
		User: user, SongID: song, PlayedAt: at, Client: "ios",
		Title: title, Artist: "A", Album: "Alb", AlbumID: "al1", CoverArt: "ca1", Duration: 200,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestGetRecentlyPlayedValidAuthJSON(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, st := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	base := time.Unix(1700000000, 0).UTC()
	seedPlay(t, st, "alice", "s1", "First", base)
	seedPlay(t, st, "alice", "s2", "Second", base.Add(time.Minute))
	seedPlay(t, st, "bob", "s3", "NotAlices", base.Add(2*time.Minute))

	resp, err := http.Get(srv.URL + "/rest/getRecentlyPlayed.view?u=alice&t=good&s=salt&c=ios&f=json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	var r recentsJSON
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r.Response.Status != "ok" {
		t.Fatalf("status = %s", r.Response.Status)
	}
	songs := r.Response.RecentlyPlayed.Song
	if len(songs) != 2 {
		t.Fatalf("want 2 songs for alice, got %d: %+v", len(songs), songs)
	}
	// Most recent first, scoped to alice.
	if songs[0].ID != "s2" || songs[1].ID != "s1" {
		t.Errorf("order/scoping wrong: %+v", songs)
	}
	if songs[1].PlayedAt != base.Unix() {
		t.Errorf("playedAt not surfaced: %+v", songs[1])
	}
}

func TestGetRecentlyPlayedRejectsBadCreds(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, st := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()
	seedPlay(t, st, "alice", "s1", "First", time.Now().UTC())

	resp, err := http.Get(srv.URL + "/rest/getRecentlyPlayed.view?u=alice&t=bad&s=salt&f=json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	var r recentsJSON
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r.Response.Status != "failed" || r.Response.Error == nil || r.Response.Error.Code != 40 {
		t.Errorf("expected wrong-credentials error, got %+v", r.Response)
	}
}

func TestGetRecentlyPlayedXMLDefault(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, st := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()
	seedPlay(t, st, "alice", "s1", "First", time.Now().UTC())

	resp, err := http.Get(srv.URL + "/rest/getRecentlyPlayed.view?u=alice&t=good&s=salt")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<subsonic-response") || !strings.Contains(string(body), `id="s1"`) {
		t.Errorf("expected xml with song, got:\n%s", body)
	}
}
