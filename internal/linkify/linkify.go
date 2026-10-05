// Package linkify finds URLs in plain-text post content (AGORA-375). It is
// the one place the backend decides what counts as a link, shared by
// outbound federation (an <a> in a Note's HTML), outbound Bluesky (a #link
// facet), and inbound conversion of a remote post's own links back into the
// plain text Agora stores.
//
// Besides full http(s):// URLs it recognizes a bare domain such as
// "bit.ly/4jFFzI0" or "example.com", the same way Bluesky and most social
// clients do, and links it as https://. The frontend's renderContent
// (frontend/src/utils/linkify.ts) and the mobile app's LinkedText apply the
// same rules, so keep the three in step.
package linkify

import (
	"regexp"
	"strings"
)

// urlRe matches a candidate link: a full http(s) URL, or a bare dotted host
// with an optional port and path. A bare candidate is only a link once Find
// has also checked its TLD and the character before it, since RE2 has no
// lookbehind.
var urlRe = regexp.MustCompile(`(?i)https?://[^\s<>"']+|[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*\.[a-z]{2,63}(?::[0-9]{2,5})?(?:/[^\s<>"']*)?`)

// bareHostRe pulls the host's final label (the TLD) out of a bare candidate.
var bareHostRe = regexp.MustCompile(`(?i)^[a-z0-9.-]*\.([a-z]{2,63})(?::[0-9]+)?(?:/|$)`)

// commonTLDs limits bare-domain linking to TLDs people actually share links
// on. A full IANA list would also link filenames such as "readme.md",
// "setup.py" or "install.sh" (all real ccTLDs), so those are deliberately
// left out; a link on one of them still works when written with https://.
var commonTLDs = map[string]bool{}

func init() {
	for _, tld := range strings.Fields(`
		com net org edu gov mil int info biz name pro
		io co ly me tv fm am gg gl to be ai app dev xyz
		online site tech blog news social page link club store shop live
		space world website art media network cloud email today life
		wiki zone lol fyi community foundation games
		us uk ca au nz ie de fr es it nl se no fi dk ch at cz pt gr hu ro
		jp cn kr tw hk sg in id my th vn ph br ar mx cl za ng ke eu ru ua
		tr il ae sa`) {
		commonTLDs[tld] = true
	}
}

// Match is one link found in text: text[Start:End] is what the reader sees,
// Href is where it goes.
type Match struct {
	Start, End int
	Href       string
}

// Find returns every link in text, in order, with trailing sentence
// punctuation trimmed off each one.
func Find(text string) []Match {
	var out []Match
	for _, loc := range urlRe.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		// Trailing punctuation likely belongs to the sentence, not the URL.
		end = start + len(strings.TrimRight(text[start:end], ".,!?)"))
		raw := text[start:end]
		if hasScheme(raw) {
			out = append(out, Match{Start: start, End: end, Href: raw})
			continue
		}
		if !bareOK(text, start, raw) {
			continue
		}
		out = append(out, Match{Start: start, End: end, Href: "https://" + raw})
	}
	return out
}

func hasScheme(s string) bool {
	l := strings.ToLower(s)
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}

// bareOK reports whether a bare-domain candidate starting at text[start] is
// a link: its TLD is a common one, and it is not the tail of some other
// token (an email address, an @handle, a #tag, a word joined by "_").
func bareOK(text string, start int, raw string) bool {
	if start > 0 && strings.IndexByte("@#+_./:-", text[start-1]) >= 0 {
		return false
	}
	if start > 0 && isWordByte(text[start-1]) {
		return false
	}
	m := bareHostRe.FindStringSubmatch(raw)
	return m != nil && commonTLDs[strings.ToLower(m[1])]
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// PlainTextLink turns one link from a remote post (a Fediverse <a>, or a
// Bluesky #link facet) into the plain text Agora stores, so that Find (and
// the frontend's matching renderer) links it to the same place again.
//
//   - Text that already links to href on its own is kept unchanged, e.g.
//     "https://example.com/foo", or "bit.ly/4jFFzI0" for https://bit.ly/4jFFzI0.
//   - Text that is a shortened form of href, such as Bluesky's
//     "example.com/long/pa...", is replaced with href, because the shortened
//     text is not a working URL.
//   - Anything else, such as "my blog", keeps its text and gets href appended
//     in parentheses.
func PlainTextLink(text, href string) string {
	text, href = strings.TrimSpace(text), strings.TrimSpace(href)
	switch {
	case href == "":
		return text
	case text == "":
		return href
	case strings.HasPrefix(text, "@"), strings.HasPrefix(text, "#"):
		return text
	}
	if m := Find(text); len(m) == 1 && m[0].Start == 0 && m[0].End == len(text) && sameURL(m[0].Href, href) {
		return text
	}
	if shortenedFormOf(text, href) {
		return href
	}
	return text + " (" + href + ")"
}

func sameURL(a, b string) bool {
	return strings.EqualFold(strings.TrimRight(a, "/"), strings.TrimRight(b, "/"))
}

// shortenedFormOf reports whether text is href with its scheme dropped
// and/or its tail cut off ("..." or "…"), the way Bluesky and Mastodon
// shorten a long URL for display.
func shortenedFormOf(text, href string) bool {
	core := strings.TrimRight(text, ".… ")
	core = strings.ToLower(stripScheme(core))
	if core == "" || !strings.Contains(core, ".") || strings.ContainsAny(core, " \t\n") {
		return false
	}
	h := strings.ToLower(stripScheme(href))
	return strings.HasPrefix(h, core) || strings.HasPrefix(strings.TrimPrefix(h, "www."), strings.TrimPrefix(core, "www."))
}

func stripScheme(s string) string {
	if i := strings.Index(s, "://"); i >= 0 && hasScheme(s) {
		return s[i+3:]
	}
	return s
}
