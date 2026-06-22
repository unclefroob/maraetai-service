// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Config holds the proxy's runtime configuration.
type Config struct {
	// ListenAddr is the address the proxy listens on, e.g. ":8080".
	ListenAddr string
	// NavidromeURL is the upstream Navidrome base URL, e.g. "http://navidrome:4533".
	NavidromeURL *url.URL
}

// Load reads configuration from the environment and validates it.
func Load() (*Config, error) {
	raw := strings.TrimSpace(os.Getenv("NAVIDROME_URL"))
	if raw == "" {
		return nil, fmt.Errorf("NAVIDROME_URL is required (e.g. http://navidrome:4533)")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("NAVIDROME_URL is not a valid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("NAVIDROME_URL must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("NAVIDROME_URL must include a host")
	}

	addr := strings.TrimSpace(os.Getenv("LISTEN_ADDR"))
	if addr == "" {
		addr = ":8080"
	}

	return &Config{
		ListenAddr:   addr,
		NavidromeURL: u,
	}, nil
}
