package proxy

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// blob is a shortened version of the real thing: a whole LRC sheet that landed
// in ID3's TEXT (lyricist) frame and became an artist row in Navidrome.
const blob = "[00:00.26]I Have Nothing\n[00:36.64]I won't hold it back again, this passion inside"

// searchUpstream stands in for Navidrome, returning a canned search3 body.
func searchUpstream(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, body)
	}))
}

func filterThrough(t *testing.T, contentType, body string) (string, string) {
	t.Helper()
	up := searchUpstream(t, contentType, body)
	defer up.Close()
	u, _ := url.Parse(up.URL)

	front := httptest.NewServer(newSearchFilter(u, slog.New(slog.DiscardHandler)))
	defer front.Close()

	resp, err := http.Get(front.URL + "/rest/search3.view?query=aga")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	return string(got), resp.Header.Get("Content-Length")
}

func TestSearchFilterDropsLyricsBlobArtistsJSON(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"subsonic-response": map[string]any{
		"status": "ok",
		"searchResult3": map[string]any{
			"artist": []any{
				map[string]any{"id": "1", "name": "AGA", "albumCount": 4},
				map[string]any{"id": "2", "name": blob},
				map[string]any{"id": "3", "name": "AGA & Gin Lee"},
			},
			"song": []any{map[string]any{"id": "9", "title": "Again"}},
		},
	}})

	got, _ := filterThrough(t, "application/json", string(body))

	if strings.Contains(got, "I won't hold it back") {
		t.Fatalf("lyrics blob survived the filter: %s", got)
	}
	var out struct {
		R struct {
			S struct {
				Artist []struct {
					ID         string `json:"id"`
					Name       string `json:"name"`
					AlbumCount int    `json:"albumCount"`
				} `json:"artist"`
				Song []struct {
					Title string `json:"title"`
				} `json:"song"`
			} `json:"searchResult3"`
		} `json:"subsonic-response"`
	}
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("unmarshal filtered body: %v", err)
	}
	if len(out.R.S.Artist) != 2 {
		t.Fatalf("want 2 artists kept, got %d", len(out.R.S.Artist))
	}
	if out.R.S.Artist[0].Name != "AGA" || out.R.S.Artist[1].Name != "AGA & Gin Lee" {
		t.Fatalf("wrong artists kept: %+v", out.R.S.Artist)
	}
	// Unrelated fields and sibling result types must survive untouched.
	if out.R.S.Artist[0].AlbumCount != 4 {
		t.Errorf("albumCount lost in the round-trip: %+v", out.R.S.Artist[0])
	}
	if len(out.R.S.Song) != 1 || out.R.S.Song[0].Title != "Again" {
		t.Errorf("song results were altered: %+v", out.R.S.Song)
	}
}

func TestSearchFilterDropsLyricsBlobArtistsXML(t *testing.T) {
	body := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<subsonic-response xmlns="http://subsonic.org/restapi" status="ok" version="1.16.1">` +
		`<searchResult3>` +
		`<artist id="1" name="AGA" albumCount="4" coverArt="ar-1"/>` +
		`<artist id="2" name="[00:36.64]I won&#39;t hold it back again"/>` +
		`<artist id="3" name="AGA &amp; Gin Lee"/>` +
		`<song id="9" title="Again"/>` +
		`</searchResult3></subsonic-response>`

	got, clen := filterThrough(t, "application/xml", body)

	if strings.Contains(got, "hold it back") {
		t.Fatalf("lyrics blob survived the filter: %s", got)
	}
	for _, want := range []string{`name="AGA"`, `coverArt="ar-1"`, `name="AGA &amp; Gin Lee"`, `<song id="9" title="Again"/>`} {
		if !strings.Contains(got, want) {
			t.Errorf("filtered XML lost %s:\n%s", want, got)
		}
	}
	if clen != "" && clen != strconv.Itoa(len(got)) {
		t.Errorf("Content-Length %s does not match body length %d", clen, len(got))
	}
}

func TestSearchFilterKeepsCleanResponseByteForByte(t *testing.T) {
	body := `{"subsonic-response":{"status":"ok","searchResult3":{"artist":[{"id":"1","name":"AGA"}]}}}`
	got, _ := filterThrough(t, "application/json", body)
	if got != body {
		t.Fatalf("clean body was rewritten:\n got %s\nwant %s", got, body)
	}
}

func TestSearchFilterPassesThroughUnparseableBody(t *testing.T) {
	body := `this is not valid json or xml at all`
	got, _ := filterThrough(t, "application/json", body)
	if got != body {
		t.Fatalf("fail-open broken: got %q want %q", got, body)
	}
}

func TestSearchFilterHandlesJSONP(t *testing.T) {
	body := `cb({"subsonic-response":{"status":"ok","searchResult3":{"artist":[` +
		`{"id":"1","name":"AGA"},{"id":"2","name":"[00:12.00]again and again"}]}}});`
	got, _ := filterThrough(t, "text/javascript", body)

	if strings.Contains(got, "again and again") {
		t.Fatalf("blob survived in JSONP: %s", got)
	}
	if !strings.HasPrefix(got, "cb(") || !strings.HasSuffix(got, ");") {
		t.Fatalf("JSONP wrapper not preserved: %s", got)
	}
}

func TestLooksLikeLyrics(t *testing.T) {
	drop := []string{
		blob,
		"[ver:v1.0]",
		"[ti:]",
		"[00:00.50]Unchained Melody",
		strings.Repeat("x", maxArtistNameLen+1),
		"two\nlines",
	}
	keep := []string{
		"AGA",
		"AGA & Gin Lee",
		"ChanWingHim & AGA",
		"張憶亞",
		"Above & Beyond",
		"Panic! At The Disco",
		"Blink-182",
		"[dunkelbunt]", // a real bracketed artist name, no LRC cue
		"12:51",        // a Strokes track title used as an artist name
	}
	for _, s := range drop {
		if !looksLikeLyrics(s) {
			t.Errorf("should have been dropped: %q", s)
		}
	}
	for _, s := range keep {
		if looksLikeLyrics(s) {
			t.Errorf("real artist name wrongly dropped: %q", s)
		}
	}
}
