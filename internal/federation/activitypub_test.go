package federation

import "testing"

// AGORA-175: threads.net sends Accept(Follow)'s "object" as a bare IRI
// string (the original Follow's id) instead of embedding the Follow object,
// which used to make handleInboundAcceptFollow silently no-op and leave the
// follow stuck on "Requested" forever.
func TestUsernameFromAcceptObject(t *testing.T) {
	const domain = "https://agora.example"

	cases := []struct {
		name   string
		object string
		want   string
	}{
		{
			name:   "embedded Follow object (Mastodon-style)",
			object: `{"type":"Follow","actor":"https://agora.example/federation/users/alice","object":"https://threads.net/users/bob"}`,
			want:   "alice",
		},
		{
			name:   "bare IRI string referencing the original Follow's id (threads.net-style)",
			object: `"https://agora.example/federation/users/alice/follows/1699999999"`,
			want:   "alice",
		},
		{
			name:   "embedded object with empty actor falls back to string parse and fails",
			object: `{"type":"Follow","actor":""}`,
			want:   "",
		},
		{
			name:   "IRI string not belonging to our instance",
			object: `"https://threads.net/users/bob/follows/1"`,
			want:   "",
		},
		{
			name:   "garbage",
			object: `42`,
			want:   "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := usernameFromAcceptObject([]byte(tc.object), domain)
			if got != tc.want {
				t.Errorf("usernameFromAcceptObject(%s) = %q, want %q", tc.object, got, tc.want)
			}
		})
	}
}

// AGORA-177: HTMLToPlainText used to strip <a> tags down to their inner text
// only, discarding the href — fine for Mastodon's auto-linked URLs (whose
// inner text, once its own nested <span>s are stripped, already is the URL)
// and for @mention/#hashtag links (whose inner text is already meaningful),
// but it silently dropped the link entirely for hand-authored anchor text
// that differs from its href, leaving bios with no way to click through.
func TestHtmlToPlainText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "Mastodon auto-linked URL split across invisible/ellipsis spans",
			in:   `check out <a href="https://example.com/foo" class="u-url" rel="nofollow noopener" target="_blank"><span class="invisible">https://</span><span class="">example.com/foo</span></a>`,
			want: "check out https://example.com/foo",
		},
		{
			name: "mention link keeps its @handle text, not the profile URL",
			in:   `hi <a href="https://instance.social/@bob" class="u-url mention">@<span>bob</span></a>`,
			want: "hi @bob",
		},
		{
			name: "hashtag link keeps its #tag text, not the tag URL",
			in:   `<a href="https://instance.social/tags/foo" class="mention hashtag" rel="tag">#<span>foo</span></a> fan`,
			want: "#foo fan",
		},
		{
			name: "hand-authored link text differing from href appends the href so it's still clickable",
			in:   `<a href="https://example.com/blog">my blog</a>`,
			want: "my blog (https://example.com/blog)",
		},
		{
			name: "empty link text falls back to the bare href",
			in:   `<a href="https://example.com"></a>`,
			want: "https://example.com",
		},
		{
			name: "br and closing p/li become newlines",
			in:   "<p>line one<br>line two</p>",
			want: "line one\nline two",
		},
		{
			name: "HTML entities are unescaped",
			in:   "Tom &amp; Jerry",
			want: "Tom & Jerry",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HTMLToPlainText(tc.in)
			if got != tc.want {
				t.Errorf("HTMLToPlainText(%s) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
