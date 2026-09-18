package proxy

import "net/url"

// authParams extracts the Subsonic auth fields from a request's query values,
// for forwarding to the navidrome client on an upstream lookup.
func authParams(q url.Values) url.Values {
	out := url.Values{}
	for _, k := range []string{"u", "t", "s", "p", "c", "v"} {
		if v := q.Get(k); v != "" {
			out.Set(k, v)
		}
	}
	return out
}
