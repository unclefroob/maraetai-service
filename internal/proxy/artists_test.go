package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type artistsJSON struct {
	Response struct {
		Status string `json:"status"`
		Error  *struct {
			Code int `json:"code"`
		} `json:"error"`
		ArtistList struct {
			Artist []struct {
				ID         string `json:"id"`
				Name       string `json:"name"`
				AlbumCount int    `json:"albumCount"`
			} `json:"artist"`
		} `json:"artistList"`
	} `json:"subsonic-response"`
}

func fetchArtists(t *testing.T, base, query string) artistsJSON {
	t.Helper()
	resp, err := http.Get(base + "/rest/getArtistList.view?" + query)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var r artistsJSON
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return r
}

func artistIDs(r artistsJSON) []string {
	out := make([]string, 0, len(r.Response.ArtistList.Artist))
	for _, a := range r.Response.ArtistList.Artist {
		out = append(out, a.ID)
	}
	return out
}

func TestGetArtistListPagesFlattenedIndex(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, _ := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// fake getArtists: AGA, Avril, Massive Attack (flattened across index groups).
	p0 := fetchArtists(t, srv.URL, "u=alice&t=good&s=salt&offset=0&count=2&f=json")
	if got := artistIDs(p0); len(got) != 2 || got[0] != "ar1" || got[1] != "ar2" {
		t.Fatalf("page 0 = %v, want [ar1 ar2]", got)
	}
	if p0.Response.ArtistList.Artist[0].AlbumCount != 3 {
		t.Errorf("albumCount not surfaced: %+v", p0.Response.ArtistList.Artist[0])
	}
	p1 := fetchArtists(t, srv.URL, "u=alice&t=good&s=salt&offset=2&count=2&f=json")
	if got := artistIDs(p1); len(got) != 1 || got[0] != "ar3" {
		t.Fatalf("page 1 = %v, want [ar3]", got)
	}
	p2 := fetchArtists(t, srv.URL, "u=alice&t=good&s=salt&offset=9&count=2&f=json")
	if got := artistIDs(p2); len(got) != 0 {
		t.Fatalf("page past end = %v, want []", got)
	}
}

func TestGetArtistListRejectsBadCreds(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, _ := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	r := fetchArtists(t, srv.URL, "u=alice&t=bad&s=salt&f=json")
	if r.Response.Status != "failed" || r.Response.Error == nil || r.Response.Error.Code != 40 {
		t.Errorf("expected wrong-credentials error, got %+v", r.Response)
	}
}
