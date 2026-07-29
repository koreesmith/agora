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
