package proxy

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unclefroob/maraetai-service/internal/store"
)

func buildProxy(t *testing.T, upstreamURL, navidromePublicURL string) http.Handler {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	u, _ := url.Parse(upstreamURL)
	return New(u, st, navidromePublicURL, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestServesWebApp(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	srv := httptest.NewServer(buildProxy(t, upstream.URL, ""))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/app/")
	if err != nil {
		t.Fatalf("get /app/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/app/ status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Maraetai") || !strings.Contains(string(body), "app.js") {
		t.Errorf("/app/ did not serve the SPA index, got:\n%s", body)
	}
}

func TestConfigEndpoint(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()

	// With a public URL configured.
	srv := httptest.NewServer(buildProxy(t, upstream.URL, "https://music.example.com"))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/config")
	if err != nil {
		t.Fatalf("get /api/config: %v", err)
	}
	defer resp.Body.Close()
	var cfg map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg["navidromeUrl"] != "https://music.example.com" {
		t.Errorf("navidromeUrl = %q, want the configured URL", cfg["navidromeUrl"])
	}

	// Unset → empty string (UI shows guidance).
	srv2 := httptest.NewServer(buildProxy(t, upstream.URL, ""))
	defer srv2.Close()
	resp2, err := http.Get(srv2.URL + "/api/config")
	if err != nil {
		t.Fatalf("get /api/config (unset): %v", err)
	}
	defer resp2.Body.Close()
	var cfg2 map[string]string
	_ = json.NewDecoder(resp2.Body).Decode(&cfg2)
	if cfg2["navidromeUrl"] != "" {
		t.Errorf("unset navidromeUrl = %q, want empty", cfg2["navidromeUrl"])
	}
}
