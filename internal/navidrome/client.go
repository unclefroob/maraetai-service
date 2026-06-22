// Package navidrome is a minimal client for the upstream Subsonic API, used to
// resolve content the proxy's own endpoints need (song metadata, an artist's
// albums/songs, top songs, similar artists, random/starred songs).
package navidrome

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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

// ArtistRef is an artist reference (id + name), e.g. a similar artist.
type ArtistRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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
			ArtistID string `json:"artistId"`
			Song     []Song `json:"song"`
		} `json:"album"`
		Error *apiError `json:"error"`
	} `json:"subsonic-response"`
}

type songListResponse struct {
	Response struct {
		Status     string    `json:"status"`
		TopSongs   *songList `json:"topSongs"`
		RandomSong *songList `json:"randomSongs"`
		Starred2   *songList `json:"starred2"`
		Error      *apiError `json:"error"`
	} `json:"subsonic-response"`
}

type songList struct {
	Song []Song `json:"song"`
}

type artistInfoResponse struct {
	Response struct {
		Status      string `json:"status"`
		ArtistInfo2 *struct {
			SimilarArtist []ArtistRef `json:"similarArtist"`
		} `json:"artistInfo2"`
		Error *apiError `json:"error"`
	} `json:"subsonic-response"`
}

// GetSong fetches metadata for a song id.
func (c *Client) GetSong(ctx context.Context, id string, auth url.Values) (*Song, error) {
	var sr songResponse
	if err := c.get(ctx, "getSong.view", url.Values{"id": {id}}, auth, &sr); err != nil {
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
	if err := c.get(ctx, "getArtist.view", url.Values{"id": {id}}, auth, &ar); err != nil {
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
	if err := c.get(ctx, "getAlbum.view", url.Values{"id": {id}}, auth, &ar); err != nil {
		return nil, err
	}
	if ar.Response.Status != "ok" || ar.Response.Album == nil {
		return nil, subsonicError(ar.Response.Error, "getAlbum")
	}
	return ar.Response.Album.Song, nil
}

// GetAlbumArtistID returns the artist id that owns an album — used to resolve an
// artist id from a play (which only carries the album id).
func (c *Client) GetAlbumArtistID(ctx context.Context, albumID string, auth url.Values) (string, error) {
	var ar albumResponse
	if err := c.get(ctx, "getAlbum.view", url.Values{"id": {albumID}}, auth, &ar); err != nil {
		return "", err
	}
	if ar.Response.Status != "ok" || ar.Response.Album == nil {
		return "", subsonicError(ar.Response.Error, "getAlbum")
	}
	return ar.Response.Album.ArtistID, nil
}

// GetTopSongs returns an artist's most popular songs (by name).
func (c *Client) GetTopSongs(ctx context.Context, artistName string, count int, auth url.Values) ([]Song, error) {
	var sr songListResponse
	extra := url.Values{"artist": {artistName}, "count": {strconv.Itoa(count)}}
	if err := c.get(ctx, "getTopSongs.view", extra, auth, &sr); err != nil {
		return nil, err
	}
	if sr.Response.Status != "ok" {
		return nil, subsonicError(sr.Response.Error, "getTopSongs")
	}
	if sr.Response.TopSongs == nil {
		return nil, nil
	}
	return sr.Response.TopSongs.Song, nil
}

// GetRandomSongs returns random songs from the library.
func (c *Client) GetRandomSongs(ctx context.Context, count int, auth url.Values) ([]Song, error) {
	var sr songListResponse
	if err := c.get(ctx, "getRandomSongs.view", url.Values{"size": {strconv.Itoa(count)}}, auth, &sr); err != nil {
		return nil, err
	}
	if sr.Response.Status != "ok" {
		return nil, subsonicError(sr.Response.Error, "getRandomSongs")
	}
	if sr.Response.RandomSong == nil {
		return nil, nil
	}
	return sr.Response.RandomSong.Song, nil
}

// GetStarredSongs returns the user's starred (favourite) songs.
func (c *Client) GetStarredSongs(ctx context.Context, auth url.Values) ([]Song, error) {
	var sr songListResponse
	if err := c.get(ctx, "getStarred2.view", nil, auth, &sr); err != nil {
		return nil, err
	}
	if sr.Response.Status != "ok" {
		return nil, subsonicError(sr.Response.Error, "getStarred2")
	}
	if sr.Response.Starred2 == nil {
		return nil, nil
	}
	return sr.Response.Starred2.Song, nil
}

// GetSimilarArtists returns artists similar to the given artist id (getArtistInfo2).
// Tolerant: this can call out to last.fm via Navidrome, so callers treat a
// failure/empty as "no discovery" rather than fatal.
func (c *Client) GetSimilarArtists(ctx context.Context, artistID string, count int, auth url.Values) ([]ArtistRef, error) {
	var ar artistInfoResponse
	extra := url.Values{"id": {artistID}, "count": {strconv.Itoa(count)}}
	if err := c.get(ctx, "getArtistInfo2.view", extra, auth, &ar); err != nil {
		return nil, err
	}
	if ar.Response.Status != "ok" || ar.Response.ArtistInfo2 == nil {
		return nil, subsonicError(ar.Response.Error, "getArtistInfo2")
	}
	return ar.Response.ArtistInfo2.SimilarArtist, nil
}

// get issues an authenticated Subsonic GET and decodes the JSON body into out.
// `extra` carries endpoint-specific params; the caller passes the originating
// request's auth params (u, t, s / p, c, v), so this reuses the same credentials.
func (c *Client) get(ctx context.Context, endpoint string, extra, auth url.Values, out any) error {
	u := *c.base
	u.Path = singleJoiningSlash(c.base.Path, "/rest/"+endpoint)

	q := url.Values{}
	for _, k := range []string{"u", "t", "s", "p", "c", "v"} {
		if v := auth.Get(k); v != "" {
			q.Set(k, v)
		}
	}
	for k, vs := range extra {
		for _, v := range vs {
			q.Set(k, v)
		}
	}
	if q.Get("c") == "" {
		q.Set("c", "maraetai-service")
	}
	if q.Get("v") == "" {
		q.Set("v", "1.16.1")
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
