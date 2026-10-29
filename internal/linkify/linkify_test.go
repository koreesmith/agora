package linkify

import (
	"reflect"
	"testing"
)

func TestFind(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string // hrefs
	}{
		{"full URL", "see https://example.com/foo.", []string{"https://example.com/foo"}},
		{"bare short link (AGORA-375)", "move on. bit.ly/4jFFzI0", []string{"https://bit.ly/4jFFzI0"}},
		{"bare domain, no path, end of sentence", "I love example.com.", []string{"https://example.com"}},
		{"bare domain in parens", "(example.org/a)", []string{"https://example.org/a"}},
		{"bare domain with port", "example.com:8080/x", []string{"https://example.com:8080/x"}},
		{"uppercase TLD", "Example.COM/x", []string{"https://Example.COM/x"}},
		{"filename with a ccTLD-looking extension", "edit readme.md and setup.py", nil},
		{"abbreviations", "e.g. this, i.e. that", nil},
		{"version numbers", "v2.0.1 and 3.14", nil},
		{"email address", "mail bob@example.com", nil},
		{"mention handle", "hi @alice.bsky.social", nil},
		{"hashtag", "#example.com", nil},
		{"word joined by underscore", "foo_bar.com", nil},
		{"unknown TLD", "Node.js and Mr.Smith", nil},
		{"both kinds", "a https://x.com/1 b youtu.be/abc", []string{"https://x.com/1", "https://youtu.be/abc"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, m := range Find(tc.in) {
				got = append(got, m.Href)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Find(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFindOffsets(t *testing.T) {
	text := "go → bit.ly/x!"
	m := Find(text)
	if len(m) != 1 || text[m[0].Start:m[0].End] != "bit.ly/x" {
		t.Fatalf("Find(%q) = %+v, want one match covering bit.ly/x", text, m)
	}
}

func TestPlainTextLink(t *testing.T) {
	cases := []struct {
		name, text, href, want string
	}{
		{"full URL text kept", "https://example.com/foo", "https://example.com/foo", "https://example.com/foo"},
		{"bare text that links to href kept (AGORA-375)", "bit.ly/4jFFzI0", "https://bit.ly/4jFFzI0", "bit.ly/4jFFzI0"},
		{"trailing slash difference ignored", "example.com", "https://example.com/", "example.com"},
		{"Bluesky-shortened text replaced by href", "nytimes.com/2026/10/05/us/some-lo...", "https://www.nytimes.com/2026/10/05/us/some-long-story.html", "https://www.nytimes.com/2026/10/05/us/some-long-story.html"},
		{"unicode ellipsis", "example.com/long/pa…", "https://example.com/long/path", "https://example.com/long/path"},
		{"bare text on uncommon TLD becomes href", "readme.md/x", "https://readme.md/x", "https://readme.md/x"},
		{"domain without www", "example.com", "https://www.example.com/x", "https://www.example.com/x"},
		{"hand-authored text appends href", "my blog", "https://example.com/blog", "my blog (https://example.com/blog)"},
		{"word that happens to be in the href appends href", "blog", "https://example.com/blog", "blog (https://example.com/blog)"},
		{"full URL text pointing elsewhere appends href", "https://a.com", "https://b.com", "https://a.com (https://b.com)"},
		{"mention kept", "@bob", "https://instance.social/@bob", "@bob"},
		{"hashtag kept", "#foo", "https://instance.social/tags/foo", "#foo"},
		{"empty text", "", "https://example.com", "https://example.com"},
		{"empty href", "hello", "", "hello"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PlainTextLink(tc.text, tc.href); got != tc.want {
				t.Errorf("PlainTextLink(%q, %q) = %q, want %q", tc.text, tc.href, got, tc.want)
			}
		})
	}
}
