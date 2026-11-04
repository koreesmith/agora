package federation

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// AGORA-453: a quote-share arrives as a Create(Note) whose text ends with
// "RE: <url of the quoted post>" (AGORA-239 added that line so Mastodon shows
// the quoted post as a link). Nothing structured says which post is quoted, so
// the receiving instance stored plain text and the card never appeared. This
// finds the quoted post, from a property that names it or from that line, and
// links the post to it the way a local quote-share already is (repost_of_id).

// apLinkRef is a JSON value that is either a URL string or an object with an
// id or href. The quote properties come in both shapes.
type apLinkRef string

func (r *apLinkRef) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*r = apLinkRef(s)
		return nil
	}
	var o struct {
		ID   string `json:"id"`
		Href string `json:"href"`
	}
	if json.Unmarshal(b, &o) == nil {
		if o.ID != "" {
			*r = apLinkRef(o.ID)
		} else {
			*r = apLinkRef(o.Href)
		}
	}
	return nil // a shape we do not understand is simply no quote
}

// apQuoteRefs are the properties other software uses to name a quoted post:
// FEP-044f "quote", Fedibird "quoteUri", Misskey "_misskey_quote" and the
// common "quoteUrl".
type apQuoteRefs struct {
	Quote        apLinkRef `json:"quote"`
	QuoteURL     apLinkRef `json:"quoteUrl"`
	QuoteURI     apLinkRef `json:"quoteUri"`
	MisskeyQuote apLinkRef `json:"_misskey_quote"`
}

func (q apQuoteRefs) first() string {
	for _, v := range []apLinkRef{q.QuoteURL, q.QuoteURI, q.MisskeyQuote, q.Quote} {
		if u := strings.TrimSpace(string(v)); strings.HasPrefix(u, "https://") {
			return u
		}
	}
	return ""
}

// reLine matches the trailing "RE: <url>" line, alone on its last line.
var reLine = regexp.MustCompile(`(?:^|\n)[ \t]*RE:[ \t]*(https?://[^\s]+)[ \t]*\s*$`)

// splitQuoteLine returns the URL on a trailing "RE:" line and the text without
// that line.
func splitQuoteLine(text string) (quoteURL, rest string) {
	m := reLine.FindStringSubmatchIndex(text)
	if m == nil {
		return "", text
	}
	return text[m[2]:m[3]], strings.TrimRight(text[:m[0]], " \t\n")
}

var (
	bskyWebPost  = regexp.MustCompile(`^https://(?:www\.)?bsky\.app/profile/(did:[a-z]+:[A-Za-z0-9._:%-]+)/post/([A-Za-z0-9]+)/?$`)
	localPostURL = regexp.MustCompile(`^/federation/users/[^/]+/posts/([0-9a-fA-F-]{36})/?$`)
)

// quotableSQL is what a post must be to be shown as someone's quote card on
// this instance: public, not in a group or page, and not itself a repost. A
// Note naming a friends-only or private post's address must not be a way to
// show it to people it was never shared with.
const quotableSQL = `visibility = 'public' AND deleted_at IS NULL AND group_id IS NULL AND page_id IS NULL AND repost_of_id IS NULL`

// resolveQuotedPost returns the id of the post at quoteURL if this instance
// holds it, or can fetch it (a Bluesky post), and may show it. "" otherwise.
func (s *Service) resolveQuotedPost(ctx context.Context, quoteURL string) string {
	var id string
	u, err := url.Parse(quoteURL)
	if err != nil || u.Scheme != "https" {
		return ""
	}

	// One of this instance's own posts.
	if own, err := url.Parse(strings.TrimRight(s.cfg.InstanceDomain, "/")); err == nil && own.Host != "" && strings.EqualFold(u.Host, own.Host) {
		if m := localPostURL.FindStringSubmatch(u.Path); m != nil {
			s.db.QueryRowContext(ctx, `SELECT id FROM posts WHERE id = $1 AND is_remote = false AND `+quotableSQL, m[1]).Scan(&id)
		}
		return id
	}

	// A remote post already stored under this object URL.
	s.db.QueryRowContext(ctx, `SELECT id FROM posts WHERE remote_post_id = $1 AND is_remote = true AND `+quotableSQL+` LIMIT 1`, quoteURL).Scan(&id)
	if id != "" {
		return id
	}

	// A Bluesky post: stored under its at:// URI, fetched if we have not seen it.
	if m := bskyWebPost.FindStringSubmatch(quoteURL); m != nil {
		atURI := "at://" + m[1] + "/app.bsky.feed.post/" + m[2]
		s.db.QueryRowContext(ctx, `SELECT id FROM posts WHERE remote_post_id = $1 AND remote_instance = 'bsky.app' AND `+quotableSQL+` LIMIT 1`, atURI).Scan(&id)
		if id != "" {
			return id
		}
		if r, ok := s.bsky.(interface {
			IngestPostByURI(ctx context.Context, atURI string) (string, error)
		}); ok {
			if got, err := r.IngestPostByURI(ctx, atURI); err == nil && got != "" {
				s.db.QueryRowContext(ctx, `SELECT id FROM posts WHERE id = $1 AND `+quotableSQL, got).Scan(&id)
			}
		}
	}
	return id
}

// linkInboundQuote turns a stored inbound post into a quote of the post it
// names. explicit is a URL from a quote property ("" if there was none); the
// "RE:" line in the stored text is the fallback, and is removed whenever the
// quote is linked so the card is not shown next to the same link. Nothing
// changes if the quoted post can't be found.
func (s *Service) linkInboundQuote(postID, explicit string) {
	var content string
	var already *string
	if err := s.db.QueryRow(`SELECT content, repost_of_id FROM posts WHERE id = $1 AND is_remote = true`, postID).Scan(&content, &already); err != nil || already != nil {
		return
	}
	lineURL, rest := splitQuoteLine(content)
	quoteURL := explicit
	if quoteURL == "" {
		quoteURL = lineURL
	}
	if quoteURL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	quotedID := s.resolveQuotedPost(ctx, quoteURL)
	if quotedID == "" || quotedID == postID {
		return
	}
	// Only the line that named this quote goes; a different trailing link is text.
	if lineURL != quoteURL {
		rest = content
	}
	s.db.Exec(`UPDATE posts SET repost_of_id = $1, content = $2 WHERE id = $3 AND repost_of_id IS NULL`, quotedID, rest, postID)
}
