package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type usersJSON struct {
	Response struct {
		Status string `json:"status"`
		Users  struct {
			User []struct {
				Username string `json:"username"`
			} `json:"user"`
		} `json:"users"`
	} `json:"subsonic-response"`
}

func usernames(r usersJSON) []string {
	out := make([]string, 0, len(r.Response.Users.User))
	for _, u := range r.Response.Users.User {
		out = append(out, u.Username)
	}
	return out
}

func TestGetUsersAdminSeesEveryoneWithPlays(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, st := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	now := time.Now().UTC()
	seedPlay(t, st, "alice", "s1", "First", now)
	seedPlay(t, st, "bob", "s2", "Second", now)
	// "admin" (the caller) has never played anything through this proxy —
	// they must still see themselves in the list.

	resp, err := http.Get(srv.URL + "/rest/getUsers.view?u=admin&t=good&s=salt&f=json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var r usersJSON
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r.Response.Status != "ok" {
		t.Fatalf("status = %q", r.Response.Status)
	}
	got := usernames(r)
	want := []string{"admin", "alice", "bob"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, u := range want {
		if got[i] != u {
			t.Errorf("got[%d] = %q, want %q (got %v)", i, got[i], u, got)
		}
	}
}

func TestGetUsersNonAdminSeesOnlyThemselves(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, st := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	now := time.Now().UTC()
	seedPlay(t, st, "alice", "s1", "First", now)
	seedPlay(t, st, "bob", "s2", "Second", now)

	resp, err := http.Get(srv.URL + "/rest/getUsers.view?u=alice&t=good&s=salt&f=json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var r usersJSON
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := usernames(r)
	if len(got) != 1 || got[0] != "alice" {
		t.Errorf("non-admin should only see themselves, got %v", got)
	}
}

func TestGetUsersRejectsBadCreds(t *testing.T) {
	var hits int32
	upstream := fakeNavidrome(t, &hits)
	defer upstream.Close()
	h, _ := teeProxy(t, upstream.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/rest/getUsers.view?u=alice&t=bad&s=salt&f=json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var r usersJSON
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r.Response.Status != "failed" {
		t.Errorf("status = %q, want failed", r.Response.Status)
	}
}
