// Package alttext normalizes image descriptions (alt text, AGORA-400) the same
// way wherever they enter: a local post, a group or page post, or a post
// arriving from the Fediverse or Bluesky.
package alttext

import "strings"

// MaxLen is Mastodon's own limit on an image description, so anything stored
// here federates to it whole.
const MaxLen = 1500

// Clean returns exactly n descriptions: trimmed, capped at MaxLen characters,
// and padded with "" for images given none, so alts[i] always describes image
// i. Extra descriptions beyond n are dropped.
func Clean(alts []string, n int) []string {
	out := make([]string, n)
	for i := 0; i < n && i < len(alts); i++ {
		a := strings.TrimSpace(alts[i])
		if r := []rune(a); len(r) > MaxLen {
			a = strings.TrimSpace(string(r[:MaxLen]))
		}
		out[i] = a
	}
	return out
}
