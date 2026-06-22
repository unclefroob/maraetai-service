// Package subsonic encodes Subsonic-API-shaped responses in the format the
// client asked for (xml default, json, or jsonp), so the Maraetai apps can
// reuse their existing Subsonic response decoders against our endpoints.
package subsonic

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/url"
)

// Protocol metadata advertised in every response.
const (
	Version    = "1.16.1"
	ServerType = "maraetai-service"
)

// Subsonic error codes used here.
const (
	ErrGeneric          = 0
	ErrRequiredParam    = 10
	ErrWrongCredentials = 40
)

// Child mirrors the Subsonic "Child" object for a song, plus a non-standard
// playedAt (unix seconds) extension carrying the time of the recorded play.
type Child struct {
	ID       string `xml:"id,attr" json:"id"`
	IsDir    bool   `xml:"isDir,attr" json:"isDir"`
	Title    string `xml:"title,attr" json:"title"`
	Album    string `xml:"album,attr,omitempty" json:"album,omitempty"`
	Artist   string `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	AlbumID  string `xml:"albumId,attr,omitempty" json:"albumId,omitempty"`
	CoverArt string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	Duration int    `xml:"duration,attr,omitempty" json:"duration,omitempty"`
	Type     string `xml:"type,attr,omitempty" json:"type,omitempty"`
	PlayedAt int64  `xml:"playedAt,attr,omitempty" json:"playedAt,omitempty"`
}

// RecentlyPlayed is the container for the getRecentlyPlayed response.
type RecentlyPlayed struct {
	Song []Child `xml:"song" json:"song,omitempty"`
}

// ArtistSongs is the container for the getArtistSongs response (every song in
// an artist's discography, in album order).
type ArtistSongs struct {
	Song []Child `xml:"song" json:"song,omitempty"`
}

type apiError struct {
	Code    int    `xml:"code,attr" json:"code"`
	Message string `xml:"message,attr" json:"message"`
}

// body holds the optional members of a response. Exactly one feature member is
// set per response (plus error on failure).
type body struct {
	Status         string          `xml:"status,attr" json:"status"`
	Version        string          `xml:"version,attr" json:"version"`
	Type           string          `xml:"type,attr" json:"type"`
	ServerVersion  string          `xml:"serverVersion,attr,omitempty" json:"serverVersion,omitempty"`
	Error          *apiError       `xml:"error,omitempty" json:"error,omitempty"`
	RecentlyPlayed *RecentlyPlayed `xml:"recentlyPlayed,omitempty" json:"recentlyPlayed,omitempty"`
	ArtistSongs    *ArtistSongs    `xml:"artistSongs,omitempty" json:"artistSongs,omitempty"`
}

type xmlEnvelope struct {
	XMLName xml.Name `xml:"subsonic-response"`
	Xmlns   string   `xml:"xmlns,attr"`
	body
}

type jsonEnvelope struct {
	Response body `json:"subsonic-response"`
}

// WriteRecentlyPlayed writes a successful getRecentlyPlayed response.
func WriteRecentlyPlayed(w http.ResponseWriter, q url.Values, songs []Child) {
	write(w, q, body{RecentlyPlayed: &RecentlyPlayed{Song: songs}})
}

// WriteArtistSongs writes a successful getArtistSongs response.
func WriteArtistSongs(w http.ResponseWriter, q url.Values, songs []Child) {
	write(w, q, body{ArtistSongs: &ArtistSongs{Song: songs}})
}

// WriteError writes a Subsonic error response. Note: Subsonic conveys errors in
// the body with an HTTP 200, so clients parse them uniformly.
func WriteError(w http.ResponseWriter, q url.Values, code int, msg string) {
	write(w, q, body{Error: &apiError{Code: code, Message: msg}})
}

func write(w http.ResponseWriter, q url.Values, b body) {
	b.Version = Version
	b.Type = ServerType
	if b.Error != nil {
		b.Status = "failed"
	} else {
		b.Status = "ok"
	}

	switch q.Get("f") {
	case "json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(jsonEnvelope{Response: b})
	case "jsonp":
		cb := q.Get("callback")
		if cb == "" {
			cb = "callback"
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		payload, _ := json.Marshal(jsonEnvelope{Response: b})
		_, _ = w.Write([]byte(cb + "("))
		_, _ = w.Write(payload)
		_, _ = w.Write([]byte(");"))
	default: // xml
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(xml.Header))
		enc := xml.NewEncoder(w)
		_ = enc.Encode(xmlEnvelope{Xmlns: "http://subsonic.org/restapi", body: b})
	}
}
