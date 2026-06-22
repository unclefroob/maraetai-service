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

type onRepeatJSON struct {
	Response struct {
		Status string `json:"status"`
		Error  *struct {
			Code int `json:"code"`
		} `json:"error"`
		OnRepeat struct {
			Song []struct {
				ID        string `json:"id"`
				PlayCount int    `json:"playCount"`
			} `json:"song"`
		} `json:"onRepeat"`
	} `json:"subsonic-response"`
}

func TestGetOnRepeatRanksByPlayCount(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, st := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	now := time.Now().UTC()
	ins := func(song string, n int) {
		for i := 0; i < n; i++ {
			_ = st.InsertPlay(context.Background(), store.Play{
				User: "alice", SongID: song, PlayedAt: now.Add(-time.Duration(i) * time.Minute), Title: song,
			})
		}
	}
	ins("hit", 5)  // on repeat
	ins("mid", 3)  // on repeat (== threshold)
	ins("once", 1) // below threshold

	resp, err := http.Get(srv.URL + "/rest/getOnRepeat.view?u=alice&t=good&s=salt&f=json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	var r onRepeatJSON
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r.Response.Status != "ok" {
		t.Fatalf("status = %s", r.Response.Status)
	}
	songs := r.Response.OnRepeat.Song
	if len(songs) != 2 {
		t.Fatalf("want 2 (hit, mid; 'once' below 3), got %d: %+v", len(songs), songs)
	}
	if songs[0].ID != "hit" || songs[0].PlayCount != 5 {
		t.Errorf("first = %+v, want hit/5", songs[0])
	}
	if songs[1].ID != "mid" || songs[1].PlayCount != 3 {
		t.Errorf("second = %+v, want mid/3", songs[1])
	}
}

func TestGetOnRepeatRejectsBadCreds(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, _ := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/rest/getOnRepeat.view?u=alice&t=bad&s=salt&f=json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	var r onRepeatJSON
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r.Response.Status != "failed" || r.Response.Error == nil || r.Response.Error.Code != 40 {
		t.Errorf("expected wrong-credentials error, got %+v", r.Response)
	}
}
