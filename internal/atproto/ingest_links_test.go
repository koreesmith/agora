package atproto

import (
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/api/bsky"
)

func linkFacet(text, display, uri string) *bsky.RichtextFacet {
	start := strings.Index(text, display)
	return &bsky.RichtextFacet{
		Index: &bsky.RichtextFacet_ByteSlice{ByteStart: int64(start), ByteEnd: int64(start + len(display))},
		Features: []*bsky.RichtextFacet_Features_Elem{{
			RichtextFacet_Link: &bsky.RichtextFacet_Link{Uri: uri},
		}},
	}
}

// AGORA-375: a Bluesky link's display text is often shortened by the
// author's app, and only the #link facet holds the real URI.
func TestTextWithLinks(t *testing.T) {
	t.Run("scheme-less short link keeps its text", func(t *testing.T) {
		text := "time to move on. bit.ly/4jFFzI0"
		rec := &bsky.FeedPost{Text: text, Facets: []*bsky.RichtextFacet{linkFacet(text, "bit.ly/4jFFzI0", "https://bit.ly/4jFFzI0")}}
		if got := textWithLinks(rec); got != text {
			t.Errorf("got %q, want %q", got, text)
		}
	})

	t.Run("truncated display text becomes the full URI, multibyte text before it", func(t *testing.T) {
		text := "café → example.com/long/pa... and dev.to/x... done"
		rec := &bsky.FeedPost{Text: text, Facets: []*bsky.RichtextFacet{
			linkFacet(text, "example.com/long/pa...", "https://example.com/long/path?q=1"),
			linkFacet(text, "dev.to/x...", "https://dev.to/xyz"),
		}}
		want := "café → https://example.com/long/path?q=1 and https://dev.to/xyz done"
		if got := textWithLinks(rec); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("custom link text appends the URI", func(t *testing.T) {
		text := "read my post here"
		rec := &bsky.FeedPost{Text: text, Facets: []*bsky.RichtextFacet{linkFacet(text, "my post", "https://example.com/p/1")}}
		want := "read my post (https://example.com/p/1) here"
		if got := textWithLinks(rec); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("malformed, overlapping and non-web facets are ignored", func(t *testing.T) {
		text := "see example.com/a..."
		bad := &bsky.RichtextFacet{Index: &bsky.RichtextFacet_ByteSlice{ByteStart: 4, ByteEnd: 999},
			Features: []*bsky.RichtextFacet_Features_Elem{{RichtextFacet_Link: &bsky.RichtextFacet_Link{Uri: "https://x.com"}}}}
		nonWeb := linkFacet(text, "see", "at://did:plc:abc/app.bsky.feed.post/1")
		good := linkFacet(text, "example.com/a...", "https://example.com/abc")
		overlap := linkFacet(text, "example.com", "https://other.com")
		rec := &bsky.FeedPost{Text: text, Facets: []*bsky.RichtextFacet{bad, nonWeb, good, overlap, nil}}
		want := "see https://example.com/abc"
		if got := textWithLinks(rec); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}
