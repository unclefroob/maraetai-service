package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net/url"

	"github.com/unclefroob/maraetai-service/internal/navidrome"
)

// errForbidden means the caller asked for another user's data without being
// a Navidrome admin.
var errForbidden = errors.New("forbidden")

// resolveTargetUser picks whose data a per-user endpoint (stats, recently
// played) should return: the authenticated caller by default, or a
// different user named in the `user` query param — but only when the caller
// is a Navidrome admin, checked via their own getUser.view (the same call
// the web app uses to decide whether to show its Admin nav item). Returns
// errForbidden if a non-admin asks for someone else's data.
func resolveTargetUser(ctx context.Context, nd *navidrome.Client, q url.Values, caller string) (string, error) {
	target := q.Get("user")
	if target == "" || target == caller {
		return caller, nil
	}
	u, err := nd.GetUser(ctx, caller, q)
	if err != nil {
		return "", err
	}
	if !u.AdminRole {
		return "", errForbidden
	}
	return target, nil
}

// isAdmin reports whether caller is a Navidrome admin, checked the same way
// resolveTargetUser does (the caller's own getUser.view). Unlike
// resolveTargetUser, a failed check here just means "not admin" — callers use
// this to decide whether to show *more* data (e.g. other known usernames),
// never to gate a write, so failing closed is safe and doesn't need its own
// error path.
func isAdmin(ctx context.Context, nd *navidrome.Client, q url.Values, caller string, log *slog.Logger) bool {
	u, err := nd.GetUser(ctx, caller, q)
	if err != nil {
		if log != nil {
			log.Error("admin check failed", "caller", caller, "err", err)
		}
		return false
	}
	return u.AdminRole
}
