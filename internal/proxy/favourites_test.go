package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type favouritesJSON struct {
	Response struct {
		Status string `json:"status"`
		Error  *struct {
			Code int `json:"code"`
		} `json:"error"`
		Favourites struct {
			Song []struct {
				ID string `json:"id"`
			} `json:"song"`
		} `json:"favourites"`
	} `json:"subsonic-response"`
}

func fetchFavourites(t *testing.T, base, query string) favouritesJSON {
	t.Helper()
	resp, err := http.Get(base + "/rest/getFavourites.view?" + query)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var r favouritesJSON
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return r
}

func ids(r favouritesJSON) []string {
	out := make([]string, 0, len(r.Response.Favourites.Song))
	for _, s := range r.Response.Favourites.Song {
		out = append(out, s.ID)
	}
	return out
}

func TestGetFavouritesPages(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, _ := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// fake getStarred2 returns fav1, fav2, fav3.
	p0 := fetchFavourites(t, srv.URL, "u=alice&t=good&s=salt&offset=0&count=2&f=json")
	if got := ids(p0); len(got) != 2 || got[0] != "fav1" || got[1] != "fav2" {
		t.Fatalf("page 0 = %v, want [fav1 fav2]", got)
	}
	p1 := fetchFavourites(t, srv.URL, "u=alice&t=good&s=salt&offset=2&count=2&f=json")
	if got := ids(p1); len(got) != 1 || got[0] != "fav3" {
		t.Fatalf("page 1 = %v, want [fav3]", got)
	}
	// Past the end → empty (client reads this as hasMore=false).
	p2 := fetchFavourites(t, srv.URL, "u=alice&t=good&s=salt&offset=4&count=2&f=json")
	if got := ids(p2); len(got) != 0 {
		t.Fatalf("page past end = %v, want []", got)
	}
}

func TestGetFavouritesRejectsBadCreds(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, _ := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	r := fetchFavourites(t, srv.URL, "u=alice&t=bad&s=salt&f=json")
	if r.Response.Status != "failed" || r.Response.Error == nil || r.Response.Error.Code != 40 {
		t.Errorf("expected wrong-credentials error, got %+v", r.Response)
	}
}
