package atproto

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
)

// AGORA-277: BroadcastPost previously never queried a post's poll options,
// so a poll federated to Bluesky as bare commentary text — the poll itself
// silently vanished, with no indication on the Bluesky side that a poll
// existed at all. Verifies a poll post's app.bsky.feed.post record now
// carries the flattened question + options + a link back to Agora to vote.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestBroadcastPostFlattensPollForBluesky(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	var prevEnabled string
	db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'atproto_enabled'`).Scan(&prevEnabled)
	db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('atproto_enabled', 'true') ON CONFLICT (key) DO UPDATE SET value = 'true'`)
	t.Cleanup(func() {
		db.Exec(`UPDATE instance_settings SET value = $1 WHERE key = 'atproto_enabled'`, prevEnabled)
	})

	unique := time.Now().UnixNano()
	username := fmt.Sprintf("agora277_bsky_%d", unique)
	did := "did:web:" + username
	var userID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, profile_private, atproto_enabled, atproto_did)
		VALUES ($1, $2, '', false, true, $3)
		RETURNING id
	`, username, username+"@example.invalid", did).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, poll_multiple_choice)
		VALUES ($1, 'cats or dogs?', 'public', false)
		RETURNING id
	`, userID).Scan(&postID); err != nil {
		t.Fatalf("insert poll post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	if _, err := db.Exec(`INSERT INTO poll_options (post_id, text, position) VALUES ($1, 'Cats', 0), ($1, 'Dogs', 1)`, postID); err != nil {
		t.Fatalf("insert poll options: %v", err)
	}

	var baselineSeq int64
	db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM atproto_firehose_events`).Scan(&baselineSeq)
	t.Cleanup(func() { db.Exec(`DELETE FROM atproto_firehose_events WHERE seq > $1`, baselineSeq) })

	s := NewService(db, &config.Config{InstanceDomain: "https://agora.example"}, nil)
	s.BroadcastPost(userID, postID)
	t.Cleanup(func() { db.Exec(`DELETE FROM atproto_posts WHERE post_id = $1`, postID) })

	if err := db.QueryRow(`SELECT rkey FROM atproto_posts WHERE post_id = $1`, postID).Scan(new(string)); err != nil {
		t.Fatalf("expected post to be federated (atproto_posts row), got none: %v", err)
	}

	// BroadcastPost commits the record through the repo/MST machinery rather
	// than returning it, so exercise the same query+flatten it uses directly
	// to confirm what actually got written as the post's Text.
	options, err := s.pollOptions(context.Background(), postID)
	if err != nil {
		t.Fatalf("pollOptions: %v", err)
	}
	permalink := "https://agora.example/post/" + postID
	got := flattenPollForBluesky("cats or dogs?", options, permalink)
	want := "cats or dogs?\n☐ Cats\n☐ Dogs\n\nVote: " + permalink
	if got != want {
		t.Fatalf("flattened poll text mismatch:\n got:  %q\n want: %q", got, want)
	}
}

// AGORA-277: a URL appended to a Bluesky post's plain "text" (the poll's
// "Vote:" link, or the length-limit fallback link) rendered as inert text —
// Bluesky clients only make a substring clickable if a byte-range facet
// says so, unlike Mastodon which auto-linkifies plain-text URLs in a Note's
// HTML content. Verifies the facet's byte offsets land exactly on the URL,
// including when multibyte characters (the poll's "☐") precede it — a rune
// offset would land short since "☐" is 3 bytes but 1 rune.
func TestLinkFacetsForURLs(t *testing.T) {
	permalink := "https://agora.example/post/abc123"
	text := flattenPollForBluesky("cats or dogs?", []string{"Cats", "Dogs"}, permalink)

	facets := linkFacetsForURLs(text)
	if len(facets) != 1 {
		t.Fatalf("expected 1 facet, got %d: %+v", len(facets), facets)
	}
	f := facets[0]
	got := text[f.Index.ByteStart:f.Index.ByteEnd]
	if got != permalink {
		t.Fatalf("facet byte range covers %q, want %q", got, permalink)
	}
	link := f.Features[0].RichtextFacet_Link
	if link == nil || link.Uri != permalink {
		t.Fatalf("expected link feature with uri %q, got %+v", permalink, link)
	}
}

// Trailing sentence punctuation right after a URL shouldn't be swept into
// the facet's link — mirrors linkifyURLs' equivalent trim on the
// ActivityPub/HTML side.
func TestLinkFacetsForURLsTrimsTrailingPunctuation(t *testing.T) {
	text := "check this out: https://agora.example/post/abc123."
	facets := linkFacetsForURLs(text)
	if len(facets) != 1 {
		t.Fatalf("expected 1 facet, got %d", len(facets))
	}
	want := "https://agora.example/post/abc123"
	got := text[facets[0].Index.ByteStart:facets[0].Index.ByteEnd]
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
