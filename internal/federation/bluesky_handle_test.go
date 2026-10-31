package federation

import "testing"

// AGORA-416: which full handles LookupUser hands to the Bluesky resolver
// instead of WebFinger.
func TestBlueskyHandleFrom(t *testing.T) {
	cases := []struct {
		raw    string
		handle string
		ok     bool
	}{
		{"pookleblinky.bsky.social", "pookleblinky.bsky.social", true},
		{"pookleblinky.bsky.social@bsky.app", "pookleblinky.bsky.social", true},
		{"Koree.Net", "koree.net", true},
		{"user@mastodon.social", "", false},
		{"first.last@mastodon.social", "", false},
		{"user@bsky.app", "", false},
		{"username", "", false},
		{"10.0.0.1", "", false},
		{"bad_label.example.com", "", false},
		{"-bad.example.com", "", false},
		{"two words.com", "", false},
	}
	for _, c := range cases {
		handle, ok := blueskyHandleFrom(c.raw)
		if ok != c.ok || handle != c.handle {
			t.Errorf("blueskyHandleFrom(%q) = %q, %v; want %q, %v", c.raw, handle, ok, c.handle, c.ok)
		}
	}
}
