// Package navidrome is a minimal client for the upstream Subsonic API, used to
// resolve song metadata that scrobble requests don't carry (they send only an
// id + time).
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

// Song is the subset of Subsonic song metadata we snapshot.
type Song struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	AlbumID  string `json:"albumId"`
	CoverArt string `json:"coverArt"`
	Duration int    `json:"duration"`
}

type songResponse struct {
	Response struct {
		Status string `json:"status"`
		Song   *Song  `json:"song"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	} `json:"subsonic-response"`
}

// GetSong fetches metadata for a song id. The caller passes the Subsonic auth
// params (u, t, s / p, c, v) from the originating request so this reuses the
// same credentials — no separate auth needed.
func (c *Client) GetSong(ctx context.Context, id string, auth url.Values) (*Song, error) {
	u := *c.base
	u.Path = singleJoiningSlash(c.base.Path, "/rest/getSong.view")

	q := url.Values{}
	// Carry through only the params getSong needs.
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
	q.Set("id", id)
	q.Set("f", "json")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getSong request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getSong: upstream status %d", resp.StatusCode)
	}

	var sr songResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("getSong decode: %w", err)
	}
	if sr.Response.Status != "ok" || sr.Response.Song == nil {
		if sr.Response.Error != nil {
			return nil, fmt.Errorf("getSong: subsonic error %d: %s",
				sr.Response.Error.Code, sr.Response.Error.Message)
		}
		return nil, fmt.Errorf("getSong: no song in response")
	}
	return sr.Response.Song, nil
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
