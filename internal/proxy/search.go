package proxy

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// maxArtistNameLen is the length past which an "artist" name is assumed to be
// junk rather than a name. The longest real names in a library run to a few
// dozen characters; the blobs that prompted this ran to ~1000.
const maxArtistNameLen = 120

// maxSearchBody caps how much of a search response is buffered for filtering.
// Anything larger is streamed through untouched rather than held in memory.
const maxSearchBody = 8 << 20

// lrcTimestamp matches an LRC cue ("[01:23.45]") or an LRC metadata tag
// ("[ti:]", "[ver:v1.0]") — the giveaway that a lyrics sheet has been parsed
// as a name.
var lrcTimestamp = regexp.MustCompile(`\[\d{1,2}:\d{2}|\[(?i:ver|ti|ar|al|by|offset|re):`)

// looksLikeLyrics reports whether an artist name is really a lyrics sheet that
// leaked in through a mistagged file.
//
// Taggers sometimes dump a whole LRC file into ID3's TEXT (lyricist) frame.
// Navidrome maps lyricist to an artist role, so the sheet becomes an artist
// row, and search3 then returns it for any query matching a word in the lyrics
// ("Aga" matches "again"). Upstream is the right place to fix that — this is
// the client-facing backstop so a single mistagged file can't pollute search
// for every app.
func looksLikeLyrics(name string) bool {
	if strings.ContainsAny(name, "\n\r") { // real artist names are single-line
		return true
	}
	if len(name) > maxArtistNameLen {
		return true
	}
	return lrcTimestamp.MatchString(name)
}

// newSearchFilter builds a reverse proxy for the Subsonic search endpoints that
// strips lyrics-blob artists out of the upstream response.
//
// It is deliberately fail-open: any response it cannot confidently parse and
// rewrite is passed through byte-for-byte, so the filter can degrade to a plain
// passthrough but never break search.
func newSearchFilter(upstream *url.URL, log *slog.Logger) http.Handler {
	rp := httputil.NewSingleHostReverseProxy(upstream)

	inner := rp.Director
	rp.Director = func(r *http.Request) {
		inner(r)
		r.Host = upstream.Host
		// Filtering needs the plain body; search responses are small, so the
		// forgone compression on this one endpoint costs nothing.
		r.Header.Set("Accept-Encoding", "identity")
	}

	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Error("upstream proxy error", "path", r.URL.Path, "err", err)
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}

	rp.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Encoding") != "" {
			return nil
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxSearchBody+1))
		resp.Body.Close()
		if err != nil {
			return err
		}
		if len(body) > maxSearchBody { // too big to vet; hand it back untouched
			restore(resp, body)
			return nil
		}

		filtered, dropped := filterSearchBody(body, resp.Header.Get("Content-Type"))
		if dropped > 0 {
			log.Info("search: dropped lyrics-blob artists", "count", dropped)
			body = filtered
		}
		restore(resp, body)
		return nil
	}

	return rp
}

// restore re-attaches a fully-read body to the response, keeping the framing
// headers consistent with its new length.
func restore(resp *http.Response, body []byte) {
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	resp.Header.Del("Content-Encoding")
}

// filterSearchBody removes lyrics-blob artists from a Subsonic search response,
// returning the rewritten body and how many entries were dropped. A body it
// cannot parse yields (nil, 0) so the caller forwards the original.
func filterSearchBody(body []byte, contentType string) ([]byte, int) {
	switch {
	case strings.Contains(contentType, "json"):
		return filterJSON(body)
	case strings.Contains(contentType, "xml"):
		return filterXML(body)
	default:
		// Content-Type is unreliable for JSONP; fall back to sniffing.
		if t := bytes.TrimLeft(body, " \t\r\n"); len(t) > 0 && t[0] == '<' {
			return filterXML(body)
		}
		return filterJSON(body)
	}
}

// filterJSON rewrites a JSON (or JSONP) body. It decodes into generic maps so
// every field Navidrome sends survives the round-trip untouched.
func filterJSON(body []byte) ([]byte, int) {
	payload, prefix, suffix := unwrapJSONP(body)

	var root map[string]any
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil, 0
	}
	resp, ok := root["subsonic-response"].(map[string]any)
	if !ok {
		return nil, 0
	}

	dropped := 0
	for _, key := range []string{"searchResult3", "searchResult2", "searchResult"} {
		result, ok := resp[key].(map[string]any)
		if !ok {
			continue
		}
		artists, ok := result["artist"].([]any)
		if !ok {
			continue
		}
		kept := make([]any, 0, len(artists))
		for _, a := range artists {
			if m, ok := a.(map[string]any); ok {
				if name, ok := m["name"].(string); ok && looksLikeLyrics(name) {
					dropped++
					continue
				}
			}
			kept = append(kept, a)
		}
		result["artist"] = kept
	}
	if dropped == 0 {
		return nil, 0
	}

	out, err := json.Marshal(root)
	if err != nil {
		return nil, 0
	}
	if prefix != "" || suffix != "" {
		out = []byte(prefix + string(out) + suffix)
	}
	return out, dropped
}

// unwrapJSONP splits a JSONP body into its callback wrapper and the JSON
// payload inside it. A plain JSON body comes back unwrapped.
func unwrapJSONP(body []byte) (payload []byte, prefix, suffix string) {
	trimmed := bytes.TrimRight(body, " \t\r\n;")
	open := bytes.IndexByte(trimmed, '(')
	if open <= 0 || !bytes.HasSuffix(trimmed, []byte(")")) {
		return body, "", ""
	}
	if ident := bytes.TrimSpace(trimmed[:open]); !isJSIdent(ident) {
		return body, "", ""
	}
	return trimmed[open+1 : len(trimmed)-1], string(trimmed[:open+1]), string(body[len(trimmed)-1:])
}

func isJSIdent(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for i, c := range string(b) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_', c == '$':
		case c == '.' && i > 0:
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// filterXML removes offending <artist> elements from an XML body by copying the
// original bytes verbatim and skipping only the elements being dropped, so no
// other element is reformatted or loses an attribute.
func filterXML(body []byte) ([]byte, int) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	var out bytes.Buffer
	copied, dropped := 0, 0

	for {
		start := dec.InputOffset()
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "artist" {
			continue
		}
		name := ""
		for _, a := range se.Attr {
			if a.Name.Local == "name" {
				name = a.Value
				break
			}
		}
		if !looksLikeLyrics(name) {
			continue
		}
		out.Write(body[copied:start])
		if err := dec.Skip(); err != nil { // past this element's closing tag
			return nil, 0
		}
		copied = int(dec.InputOffset())
		dropped++
	}
	if dropped == 0 {
		return nil, 0
	}
	out.Write(body[copied:])
	return out.Bytes(), dropped
}
