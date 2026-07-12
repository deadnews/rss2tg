package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAllow(t *testing.T) {
	tests := []struct {
		name             string
		title            string
		include, exclude []string
		want             bool
	}{
		{"empty filters allow", "Go 1.26 release", nil, nil, true},
		{"exclude blocks match", "AI is overhyped", nil, []string{"ai"}, false},
		{"exclude case-insensitive", "AI news", nil, []string{"ai"}, false},
		{"exclude matches inside words", "Email tips", nil, []string{"ai"}, false},
		{"exclude matches version suffix", "v3.0.0-beta1", nil, []string{"beta"}, false},
		{"include allows match", "Go 1.26", []string{"go"}, nil, true},
		{"include rejects non-match", "Rust 2.0", []string{"go"}, nil, false},
		{"include matches inside words", "Going places", []string{"go"}, nil, true},
		{"exclude wins over include", "Go AI framework", []string{"go"}, []string{"ai"}, false},
		{"include OR semantics", "Rust release", []string{"go", "rust"}, nil, true},
		{"exclude OR semantics", "Go AI", nil, []string{"crypto", "ai"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, allow(tc.title, tc.include, tc.exclude))
		})
	}
}

func TestParseFilterArg(t *testing.T) {
	t.Run("single term", func(t *testing.T) {
		got, ok := parseFilterArg("crypto")
		assert.True(t, ok)
		assert.Equal(t, []string{"crypto"}, got)
	})

	t.Run("comma-separated, lowercased, trimmed", func(t *testing.T) {
		got, ok := parseFilterArg(" AI, Crypto , GO ")
		assert.True(t, ok)
		assert.Equal(t, []string{"ai", "crypto", "go"}, got)
	})

	t.Run("dedupes case-insensitively", func(t *testing.T) {
		got, ok := parseFilterArg("ai,AI,Ai")
		assert.True(t, ok)
		assert.Equal(t, []string{"ai"}, got)
	})

	t.Run("accepts punctuation", func(t *testing.T) {
		got, ok := parseFilterArg("c++,go")
		assert.True(t, ok)
		assert.Equal(t, []string{"c++", "go"}, got)
	})

	t.Run("rejects empty input", func(t *testing.T) {
		_, ok := parseFilterArg("")
		assert.False(t, ok)

		_, ok = parseFilterArg(", ,")
		assert.False(t, ok)
	})
}
