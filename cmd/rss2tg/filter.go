package main

import (
	"slices"
	"strings"
)

// allow reports whether title passes the filters.
// Substring, case-insensitive; exclude wins.
func allow(title string, include, exclude []string) bool {
	t := strings.ToLower(title)
	matches := func(w string) bool { return strings.Contains(t, w) }
	if slices.ContainsFunc(exclude, matches) {
		return false
	}
	return len(include) == 0 || slices.ContainsFunc(include, matches)
}

// parseFilterArg parses a comma-separated term list, lowercased and deduped.
func parseFilterArg(raw string) ([]string, bool) {
	var terms []string
	for p := range strings.SplitSeq(raw, ",") {
		term := strings.ToLower(strings.TrimSpace(p))
		if term != "" && !slices.Contains(terms, term) {
			terms = append(terms, term)
		}
	}
	return terms, len(terms) > 0
}
