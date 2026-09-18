package proxy

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTrackVideoHandlerForTest(t *testing.T, upstreamURL, videosDir string) http.Handler {
	t.Helper()
	u, err := url.Parse(upstreamURL)
	if err != nil {
		t.Fatalf("parse upstream: %v", err)
	}
	return New(u, nil, "", videosDir, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestTrackVideoServesSongLevelClip(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "song123.mp4"), "song-clip-bytes")

	h := newTrackVideoHandlerForTest(t, upstream.URL, dir)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/rest/getTrackVideo?id=song123&u=alice&t=tok&s=salt")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "song-clip-bytes" {
		t.Errorf("body = %q, want song-clip-bytes", body)
	}
}

func TestTrackVideoFallsBackToAlbumClip(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()

	dir := t.TempDir()
	// No song123.mp4 — only the album (alb9, per fakeNavidrome's getSong stub) has one.
	writeFile(t, filepath.Join(dir, "alb9.mp4"), "album-clip-bytes")

	h := newTrackVideoHandlerForTest(t, upstream.URL, dir)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/rest/getTrackVideo?id=song123&u=alice&t=tok&s=salt")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "album-clip-bytes" {
		t.Errorf("body = %q, want album-clip-bytes", body)
	}
}

func TestTrackVideoNotFound(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()

	dir := t.TempDir() // empty — neither song nor album clip exists

	h := newTrackVideoHandlerForTest(t, upstream.URL, dir)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/rest/getTrackVideo?id=song123&u=alice&t=tok&s=salt")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestTrackVideoRejectsMalformedID(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()

	dir := t.TempDir()
	// A sentinel file one directory above videosDir that must never be
	// reachable through the endpoint, however the id is spelled.
	sentinelDir := filepath.Dir(dir)
	writeFile(t, filepath.Join(sentinelDir, "secret.mp4"), "should-never-be-served")
	t.Cleanup(func() { _ = os.Remove(filepath.Join(sentinelDir, "secret.mp4")) })

	h := newTrackVideoHandlerForTest(t, upstream.URL, dir)
	srv := httptest.NewServer(h)
	defer srv.Close()

	cases := []string{
		"../secret",
		"../../etc/passwd",
		"..%2Fsecret",
		"%2e%2e%2fsecret",
		"a/b",
		"",
		strings.Repeat("a", 200), // oversized (>128 char limit)
	}
	for _, id := range cases {
		resp, err := http.Get(srv.URL + "/rest/getTrackVideo?id=" + url.QueryEscape(id) + "&u=alice&t=tok&s=salt")
		if err != nil {
			t.Fatalf("get(%q): %v", id, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Errorf("id=%q: status = 200 (body %q), want rejected (400 or 404), never a served file", id, body)
		}
		if string(body) == "should-never-be-served" {
			t.Errorf("id=%q: served the sentinel file outside videosDir!", id)
		}
	}
}

func TestTrackVideoRequiresAuth(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "song123.mp4"), "song-clip-bytes")

	h := newTrackVideoHandlerForTest(t, upstream.URL, dir)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/rest/getTrackVideo?id=song123&u=alice&t=bad&s=salt")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestTrackVideoSupportsRangeRequests(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "song123.mp4"), "0123456789")

	h := newTrackVideoHandlerForTest(t, upstream.URL, dir)
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/rest/getTrackVideo?id=song123&u=alice&t=tok&s=salt", nil)
	req.Header.Set("Range", "bytes=0-4")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "01234" {
		t.Errorf("body = %q, want 01234", body)
	}
}

func TestTrackVideoRefusesSymlinks(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()

	dir := t.TempDir()
	outsideTarget := filepath.Join(t.TempDir(), "secret.mp4")
	writeFile(t, outsideTarget, "should-never-be-served-via-symlink")
	if err := os.Symlink(outsideTarget, filepath.Join(dir, "song123.mp4")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	h := newTrackVideoHandlerForTest(t, upstream.URL, dir)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/rest/getTrackVideo?id=song123&u=alice&t=tok&s=salt")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		t.Errorf("status = 200 (body %q), want the symlink refused (404)", body)
	}
	if string(body) == "should-never-be-served-via-symlink" {
		t.Error("served the symlink target outside videosDir!")
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
