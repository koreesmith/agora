package atproto

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
)

// AGORA-373: editing an external_only post must still reach Bluesky.
// BroadcastPostUpdate previously hard-required visibility == "public", which
// an external_only post (stored 'private') never satisfies, so every edit
// was silently dropped.
func TestBroadcastPostUpdateDeliversExternalOnlyPost(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	var prevEnabled string
	db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'atproto_enabled'`).Scan(&prevEnabled)
	db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('atproto_enabled', 'true') ON CONFLICT (key) DO UPDATE SET value = 'true'`)
	t.Cleanup(func() {
		db.Exec(`UPDATE instance_settings SET value = $1 WHERE key = 'atproto_enabled'`, prevEnabled)
	})

	unique := time.Now().UnixNano()
	username := fmt.Sprintf("agora373_bsky_%d", unique)
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
		INSERT INTO posts (author_id, content, visibility, external_only)
		VALUES ($1, 'only on bluesky', 'private', true)
		RETURNING id
	`, userID).Scan(&postID); err != nil {
		t.Fatalf("insert post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	s := NewService(db, &config.Config{InstanceDomain: "https://agora.example"}, nil)
	s.BroadcastPost(userID, postID)
	t.Cleanup(func() { db.Exec(`DELETE FROM atproto_posts WHERE post_id = $1`, postID) })

	var firstCid string
	if err := db.QueryRow(`SELECT record_cid FROM atproto_posts WHERE post_id = $1`, postID).Scan(&firstCid); err != nil {
		t.Fatalf("expected the post to already be federated before editing it: %v", err)
	}

	db.Exec(`UPDATE posts SET content = 'edited, still only on bluesky' WHERE id = $1`, postID)
	s.BroadcastPostUpdate(userID, postID)

	var editedCid string
	if err := db.QueryRow(`SELECT record_cid FROM atproto_posts WHERE post_id = $1`, postID).Scan(&editedCid); err != nil {
		t.Fatalf("query atproto_posts after edit: %v", err)
	}
	if editedCid == firstCid {
		t.Fatalf("expected the edit to rewrite the Bluesky record (new CID), got the same CID %q — BroadcastPostUpdate silently no-opped", editedCid)
	}
}

// AGORA-373: pollInboundReactions' own target query must include an
// external_only post — it was previously excluded by a hard
// visibility = 'public' filter, which would have silently dropped every
// Like/Repost polled from Bluesky for one.
func TestPollInboundReactionsTargetsIncludeExternalOnly(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	unique := time.Now().UnixNano()
	username := fmt.Sprintf("agora373_reactpoll_%d", unique)
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
		INSERT INTO posts (author_id, content, visibility, external_only)
		VALUES ($1, 'only on bluesky', 'private', true)
		RETURNING id
	`, userID).Scan(&postID); err != nil {
		t.Fatalf("insert post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	if _, err := db.Exec(`
		INSERT INTO atproto_posts (post_id, user_id, rkey, record_cid) VALUES ($1, $2, 'rkey123', 'bafyreitest')
	`, postID, userID); err != nil {
		t.Fatalf("insert atproto_posts row: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM atproto_posts WHERE post_id = $1`, postID) })

	// Same query pollInboundReactions runs, exercised directly rather than
	// through the full poll (which reaches out to the live Bluesky AppView).
	rows, err := db.Query(`
		SELECT ap.post_id
		FROM atproto_posts ap
		JOIN posts p ON p.id = ap.post_id
		JOIN users u ON u.id = ap.user_id
		WHERE p.deleted_at IS NULL AND (p.visibility = 'public' OR p.external_only = true)
		  AND u.profile_private = false AND u.atproto_enabled = true
		  AND ap.post_id = $1
	`, postID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("expected the external_only post to be a reaction-poll target, got none")
	}
}
