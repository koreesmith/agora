package atproto

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
)

// AGORA-370: a post created with federate_atproto: false must never produce
// a Bluesky record.
func TestBroadcastPostSkipsWhenFederateATProtoFalse(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	var prevEnabled string
	db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'atproto_enabled'`).Scan(&prevEnabled)
	db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('atproto_enabled', 'true') ON CONFLICT (key) DO UPDATE SET value = 'true'`)
	t.Cleanup(func() {
		db.Exec(`UPDATE instance_settings SET value = $1 WHERE key = 'atproto_enabled'`, prevEnabled)
	})

	unique := time.Now().UnixNano()
	username := fmt.Sprintf("agora370_bsky_%d", unique)
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
		INSERT INTO posts (author_id, content, visibility, federate_atproto)
		VALUES ($1, 'bluesky says no', 'public', false)
		RETURNING id
	`, userID).Scan(&postID); err != nil {
		t.Fatalf("insert post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	s := NewService(db, &config.Config{InstanceDomain: "https://agora.example"}, nil)
	s.BroadcastPost(userID, postID)
	t.Cleanup(func() { db.Exec(`DELETE FROM atproto_posts WHERE post_id = $1`, postID) })

	var rkey string
	err := db.QueryRow(`SELECT rkey FROM atproto_posts WHERE post_id = $1`, postID).Scan(&rkey)
	if err == nil {
		t.Fatalf("expected no atproto_posts row for a federate_atproto:false post, got rkey %q", rkey)
	}
}
