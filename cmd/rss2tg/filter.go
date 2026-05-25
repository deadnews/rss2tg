package main

import (
	"strings"
	"unicode"
)

// allow reports whether title passes the filters.
// Whole-word, case-insensitive; exclude wins.
func allow(title string, include, exclude []string) bool {
	if len(include) == 0 && len(exclude) == 0 {
		return true
	}
	words := tokenize(title)
	for _, w := range exclude {
		if _, ok := words[w]; ok {
			return false
		}
	}
	if len(include) == 0 {
		return true
	}
	for _, w := range include {
		if _, ok := words[w]; ok {
			return true
		}
	}
	return false
}

// tokenize returns the set of lowercased letter/digit runs in s.
func tokenize(s string) map[string]struct{} {
	out := make(map[string]struct{})
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out[cur.String()] = struct{}{}
			cur.Reset()
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(unicode.ToLower(r))
		} else {
			flush()
		}
	}
	flush()
	return out
}

// parseFilterArg parses a comma-separated word list into normalized tokens.
// Rejects punctuation — it would never match under whole-word tokenization.
func parseFilterArg(raw string) ([]string, bool) {
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		for _, r := range p {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				return nil, false
			}
		}
		word := strings.ToLower(p)
		if _, dup := seen[word]; dup {
			continue
		}
		seen[word] = struct{}{}
		out = append(out, word)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}
