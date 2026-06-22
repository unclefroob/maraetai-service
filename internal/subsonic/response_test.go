package subsonic

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestWriteRecentlyPlayedJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	q := url.Values{"f": {"json"}}
	WriteRecentlyPlayed(rec, q, []Child{{ID: "s1", Title: "Angel", Artist: "Massive Attack", Duration: 379, PlayedAt: 1700000000}})

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}

	var env struct {
		R struct {
			Status         string `json:"status"`
			Version        string `json:"version"`
			Type           string `json:"type"`
			RecentlyPlayed struct {
				Song []Child `json:"song"`
			} `json:"recentlyPlayed"`
		} `json:"subsonic-response"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, rec.Body.String())
	}
	if env.R.Status != "ok" || env.R.Type != ServerType {
		t.Errorf("envelope = %+v", env.R)
	}
	if len(env.R.RecentlyPlayed.Song) != 1 || env.R.RecentlyPlayed.Song[0].Title != "Angel" {
		t.Errorf("songs = %+v", env.R.RecentlyPlayed.Song)
	}
	if env.R.RecentlyPlayed.Song[0].PlayedAt != 1700000000 {
		t.Errorf("playedAt not encoded: %+v", env.R.RecentlyPlayed.Song[0])
	}
}

func TestWriteRecentlyPlayedXMLDefault(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteRecentlyPlayed(rec, url.Values{}, []Child{{ID: "s1", Title: "Angel"}})

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "xml") {
		t.Errorf("default content-type = %q, want xml", ct)
	}
	out := rec.Body.String()
	for _, want := range []string{
		`<subsonic-response`, `xmlns="http://subsonic.org/restapi"`,
		`status="ok"`, `<recentlyPlayed>`, `<song`, `id="s1"`, `title="Angel"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("xml missing %q in:\n%s", want, out)
		}
	}
}

func TestWriteErrorJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, url.Values{"f": {"json"}}, ErrWrongCredentials, "Wrong username or password")

	var env struct {
		R struct {
			Status string `json:"status"`
			Error  struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"subsonic-response"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.R.Status != "failed" || env.R.Error.Code != ErrWrongCredentials {
		t.Errorf("error envelope = %+v", env.R)
	}
}

func TestWriteJSONP(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteRecentlyPlayed(rec, url.Values{"f": {"jsonp"}, "callback": {"cb42"}}, nil)
	body := rec.Body.String()
	if !strings.HasPrefix(body, "cb42(") || !strings.HasSuffix(body, ");") {
		t.Errorf("jsonp wrapper wrong: %s", body)
	}
}
