// Package auth validates Subsonic credentials by forwarding them to the
// upstream Navidrome server. Navidrome stays the source of truth for identity;
// this service stores no passwords and keeps no user table.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// ErrUnauthorized means the upstream rejected the credentials.
var ErrUnauthorized = errors.New("unauthorized")

// ErrMissingParams means the request lacked a username.
var ErrMissingParams = errors.New("missing auth params")

// Validator checks Subsonic auth params against upstream ping.view and caches
// successful validations briefly to avoid re-pinging on bursts.
type Validator struct {
	base *url.URL
	http *http.Client
	ttl  time.Duration

	mu    sync.Mutex
	cache map[string]time.Time // key -> expiry
}

// NewValidator returns a validator for the given upstream base URL.
func NewValidator(base *url.URL) *Validator {
	return &Validator{
		base:  base,
		http:  &http.Client{Timeout: 10 * time.Second},
		ttl:   60 * time.Second,
		cache: make(map[string]time.Time),
	}
}

// Validate verifies the Subsonic auth params (u + t/s or p) against upstream
// and returns the authenticated username. The full credential tuple is the
// cache key, so a cache hit only short-circuits identical, still-valid creds —
// it never lets a different token through for a user.
func (v *Validator) Validate(ctx context.Context, q url.Values) (string, error) {
	user := q.Get("u")
	if user == "" {
		return "", ErrMissingParams
	}

	key := user + "\x00" + q.Get("t") + "\x00" + q.Get("s") + "\x00" + q.Get("p")
	if v.cachedValid(key) {
		return user, nil
	}

	if err := v.ping(ctx, q); err != nil {
		return "", err
	}

	v.mu.Lock()
	v.cache[key] = time.Now().Add(v.ttl)
	v.mu.Unlock()
	return user, nil
}

func (v *Validator) cachedValid(key string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	exp, ok := v.cache[key]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(v.cache, key)
		return false
	}
	return true
}

func (v *Validator) ping(ctx context.Context, q url.Values) error {
	u := *v.base
	u.Path = joinPath(v.base.Path, "/rest/ping.view")

	pq := url.Values{}
	for _, k := range []string{"u", "t", "s", "p", "c", "v"} {
		if val := q.Get(k); val != "" {
			pq.Set(k, val)
		}
	}
	if pq.Get("c") == "" {
		pq.Set("c", "maraetai-service")
	}
	if pq.Get("v") == "" {
		pq.Set("v", "1.16.1")
	}
	pq.Set("f", "json")
	u.RawQuery = pq.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := v.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ErrUnauthorized
	}

	var pr struct {
		Response struct {
			Status string `json:"status"`
		} `json:"subsonic-response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return err
	}
	if pr.Response.Status != "ok" {
		return ErrUnauthorized
	}
	return nil
}

func joinPath(a, b string) string {
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
