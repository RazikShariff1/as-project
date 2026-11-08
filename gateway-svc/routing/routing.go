// Package routing parses the gateway's public paths and matches path patterns.
package routing

import "strings"

// Prefix is where every proxied request starts: /api/{service}/{path...}.
const Prefix = "/api/"

// Parse splits /api/{service}/{rest...} into the service name and the path the backend sees (always starting with "/").
// ok is false when there is no service segment.
func Parse(p string) (service, rest string, ok bool) {
	trimmed := strings.TrimPrefix(p, Prefix)
	if trimmed == p || trimmed == "" {
		return "", "", false
	}

	service, tail, _ := strings.Cut(trimmed, "/")
	if service == "" {
		return "", "", false
	}

	return service, "/" + tail, true
}

// Match reports whether path fits pattern and returns the {name} segments it captured. A {name} segment matches any one
// non-empty segment, and a trailing /** matches one or more further segments. Anything else must be equal.
func Match(pattern, path string) (params map[string]string, ok bool) {
	pat := strings.Split(strings.Trim(pattern, "/"), "/")
	got := strings.Split(strings.Trim(path, "/"), "/")
	rest := false

	if n := len(pat); n > 0 && pat[n-1] == "**" {
		pat, rest = pat[:n-1], true
	}

	if len(got) < len(pat) || (!rest && len(got) != len(pat)) || (rest && len(got) == len(pat)) {
		return nil, false
	}

	params = map[string]string{}

	for i, seg := range pat {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			if got[i] == "" {
				return nil, false
			}

			params[seg[1:len(seg)-1]] = got[i]

			continue
		}

		if seg != got[i] {
			return nil, false
		}
	}

	return params, true
}
