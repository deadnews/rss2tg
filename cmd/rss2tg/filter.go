package main

import "strings"

// allow reports whether title passes the filters.
// Substring, case-insensitive; exclude wins.
func allow(title string, include, exclude []string) bool {
	if len(include) == 0 && len(exclude) == 0 {
		return true
	}
	t := strings.ToLower(title)
	for _, w := range exclude {
		if strings.Contains(t, w) {
			return false
		}
	}
	if len(include) == 0 {
		return true
	}
	for _, w := range include {
		if strings.Contains(t, w) {
			return true
		}
	}
	return false
}

// parseFilterArg parses a comma-separated term list, lowercased and deduped.
func parseFilterArg(raw string) ([]string, bool) {
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		term := strings.ToLower(p)
		if _, dup := seen[term]; dup {
			continue
		}
		seen[term] = struct{}{}
		out = append(out, term)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}
