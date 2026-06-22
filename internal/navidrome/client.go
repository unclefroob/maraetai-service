// Package navidrome is a minimal client for the upstream Subsonic API, used to
// resolve content (song metadata, an artist's albums, an album's songs) that
// the proxy's own endpoints need.
package navidrome

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Client talks to a Navidrome/Subsonic server.
type Client struct {
	base *url.URL
	http *http.Client
}

// New returns a client for the given upstream base URL.
func New(base *url.URL) *Client {
	return &Client{
		base: base,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// Song is the subset of Subsonic song metadata we use.
type Song struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	AlbumID  string `json:"albumId"`
	CoverArt string `json:"coverArt"`
	Duration int    `json:"duration"`
}

// Album is an album stub. Year lets the caller order a discography.
type Album struct {
	ID   string `json:"id"`
	Year int    `json:"year"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type songResponse struct {
	Response struct {
		Status string    `json:"status"`
		Song   *Song     `json:"song"`
		Error  *apiError `json:"error"`
	} `json:"subsonic-response"`
}

type artistResponse struct {
	Response struct {
		Status string `json:"status"`
		Artist *struct {
			Album []Album `json:"album"`
		} `json:"artist"`
		Error *apiError `json:"error"`
	} `json:"subsonic-response"`
}

type albumResponse struct {
	Response struct {
		Status string `json:"status"`
		Album  *struct {
			Song []Song `json:"song"`
		} `json:"album"`
		Error *apiError `json:"error"`
	} `json:"subsonic-response"`
}

// GetSong fetches metadata for a song id.
func (c *Client) GetSong(ctx context.Context, id string, auth url.Values) (*Song, error) {
	var sr songResponse
	if err := c.get(ctx, "getSong.view", id, auth, &sr); err != nil {
		return nil, err
	}
	if sr.Response.Status != "ok" || sr.Response.Song == nil {
		return nil, subsonicError(sr.Response.Error, "getSong")
	}
	return sr.Response.Song, nil
}

// GetArtist returns an artist's album stubs, in the server's order.
func (c *Client) GetArtist(ctx context.Context, id string, auth url.Values) ([]Album, error) {
	var ar artistResponse
	if err := c.get(ctx, "getArtist.view", id, auth, &ar); err != nil {
		return nil, err
	}
	if ar.Response.Status != "ok" || ar.Response.Artist == nil {
		return nil, subsonicError(ar.Response.Error, "getArtist")
	}
	return ar.Response.Artist.Album, nil
}

// GetAlbum returns the songs of an album, in track order.
func (c *Client) GetAlbum(ctx context.Context, id string, auth url.Values) ([]Song, error) {
	var ar albumResponse
	if err := c.get(ctx, "getAlbum.view", id, auth, &ar); err != nil {
		return nil, err
	}
	if ar.Response.Status != "ok" || ar.Response.Album == nil {
		return nil, subsonicError(ar.Response.Error, "getAlbum")
	}
	return ar.Response.Album.Song, nil
}

// get issues an authenticated Subsonic GET and decodes the JSON body into out.
// The caller passes the originating request's auth params (u, t, s / p, c, v),
// so this reuses the same credentials — no separate auth.
func (c *Client) get(ctx context.Context, endpoint, id string, auth url.Values, out any) error {
	u := *c.base
	u.Path = singleJoiningSlash(c.base.Path, "/rest/"+endpoint)

	q := url.Values{}
	for _, k := range []string{"u", "t", "s", "p", "c", "v"} {
		if v := auth.Get(k); v != "" {
			q.Set(k, v)
		}
	}
	if q.Get("c") == "" {
		q.Set("c", "maraetai-service")
	}
	if q.Get("v") == "" {
		q.Set("v", "1.16.1")
	}
	if id != "" {
		q.Set("id", id)
	}
	q.Set("f", "json")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s request: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: upstream status %d", endpoint, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s decode: %w", endpoint, err)
	}
	return nil
}

func subsonicError(e *apiError, endpoint string) error {
	if e != nil {
		return fmt.Errorf("%s: subsonic error %d: %s", endpoint, e.Code, e.Message)
	}
	return fmt.Errorf("%s: empty or failed response", endpoint)
}

func singleJoiningSlash(a, b string) string {
	switch {
	case a == "" || a == "/":
		return b
	case a[len(a)-1] == '/' && b[0] == '/':
		return a + b[1:]
	case a[len(a)-1] != '/' && b[0] != '/':
		return a + "/" + b
	}
	return a + b
}
