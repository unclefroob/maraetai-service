package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStatsEndpointValidAuth(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, st := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	now := time.Now().UTC()
	seedPlay(t, st, "alice", "s1", "First", now)
	seedPlay(t, st, "alice", "s1", "First", now.Add(-time.Hour))
	seedPlay(t, st, "alice", "s2", "Second", now.Add(-2*time.Hour))

	resp, err := http.Get(srv.URL + "/api/stats?u=alice&t=good&s=salt&days=30")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var s struct {
		TotalPlays    int `json:"totalPlays"`
		DistinctSongs int `json:"distinctSongs"`
		TopSongs      []struct {
			SongID string `json:"songId"`
			Plays  int    `json:"plays"`
		} `json:"topSongs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.TotalPlays != 3 || s.DistinctSongs != 2 {
		t.Errorf("totals wrong: %+v", s)
	}
	if len(s.TopSongs) == 0 || s.TopSongs[0].SongID != "s1" || s.TopSongs[0].Plays != 2 {
		t.Errorf("topSongs wrong: %+v", s.TopSongs)
	}
}

func TestStatsEndpointRejectsBadCreds(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, _ := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/stats?u=alice&t=bad&s=salt")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}
