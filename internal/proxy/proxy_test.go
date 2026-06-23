package proxy

import (
	"bufio"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func newProxy(t *testing.T, upstream *httptest.Server) http.Handler {
	t.Helper()
	u, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream: %v", err)
	}
	return New(u, nil, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestForwardsTransparently(t *testing.T) {
	var gotPath, gotQuery, gotHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotHost = r.URL.Path, r.URL.RawQuery, r.Host
		w.Header().Set("X-From-Upstream", "yes")
		_, _ = io.WriteString(w, "pong")
	}))
	defer upstream.Close()

	h := newProxy(t, upstream)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/rest/ping.view?u=alice&f=json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if string(body) != "pong" {
		t.Errorf("body = %q, want pong", body)
	}
	if gotPath != "/rest/ping.view" {
		t.Errorf("upstream path = %q", gotPath)
	}
	if gotQuery != "u=alice&f=json" {
		t.Errorf("upstream query = %q", gotQuery)
	}
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")
	if gotHost != upstreamHost {
		t.Errorf("upstream Host = %q, want %q", gotHost, upstreamHost)
	}
	if resp.Header.Get("X-From-Upstream") != "yes" {
		t.Error("upstream response header not propagated")
	}
}

func TestHealthzDoesNotHitUpstream(t *testing.T) {
	hit := false
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit = true
	}))
	defer upstream.Close()

	srv := httptest.NewServer(newProxy(t, upstream))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if hit {
		t.Error("/healthz reached upstream, should be served locally")
	}
}

// TestStreamsIncrementally verifies the proxy flushes chunks as they arrive
// rather than buffering the whole body — the property audio streaming relies on.
func TestStreamsIncrementally(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			t.Error("upstream writer not a flusher")
			return
		}
		_, _ = io.WriteString(w, "chunk1\n")
		fl.Flush()
		<-release // hold the response open
		_, _ = io.WriteString(w, "chunk2\n")
		fl.Flush()
	}))
	defer upstream.Close()

	srv := httptest.NewServer(newProxy(t, upstream))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/rest/stream.view")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	r := bufio.NewReader(resp.Body)
	first := make(chan string, 1)
	go func() {
		line, _ := r.ReadString('\n')
		first <- line
	}()

	select {
	case line := <-first:
		if line != "chunk1\n" {
			t.Errorf("first chunk = %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first chunk did not arrive before upstream finished — response was buffered")
	}
	close(release)
}
