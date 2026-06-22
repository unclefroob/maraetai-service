package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type artistSongsJSON struct {
	Response struct {
		Status string `json:"status"`
		Error  *struct {
			Code int `json:"code"`
		} `json:"error"`
		ArtistSongs struct {
			Song []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"song"`
		} `json:"artistSongs"`
	} `json:"subsonic-response"`
}

func TestGetArtistSongsAggregatesInAlbumOrder(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, _ := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/rest/getArtistSongs.view?u=alice&t=good&s=salt&id=art1&f=json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	var r artistSongsJSON
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r.Response.Status != "ok" {
		t.Fatalf("status = %s", r.Response.Status)
	}
	songs := r.Response.ArtistSongs.Song
	if len(songs) != 3 {
		t.Fatalf("want 3 songs across 2 albums, got %d: %+v", len(songs), songs)
	}
	// Newest-first: al2 (2010 → s3) before al1 (1998 → s1, s2).
	got := []string{songs[0].ID, songs[1].ID, songs[2].ID}
	want := []string{"s3", "s1", "s2"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("order = %v, want %v", got, want)
			break
		}
	}
}

func TestGetArtistSongsMissingID(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, _ := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/rest/getArtistSongs.view?u=alice&t=good&s=salt&f=json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	var r artistSongsJSON
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r.Response.Status != "failed" || r.Response.Error == nil || r.Response.Error.Code != 10 {
		t.Errorf("expected missing-param error (10), got %+v", r.Response)
	}
}

func TestGetArtistSongsXMLDefault(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, _ := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/rest/getArtistSongs.view?u=alice&t=good&s=salt&id=art1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "<subsonic-response") || !strings.Contains(body, "<artistSongs>") || !strings.Contains(body, `id="s1"`) {
		t.Errorf("expected xml artistSongs, got:\n%s", body)
	}
}
