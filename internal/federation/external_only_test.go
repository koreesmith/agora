package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-371: a post stored visibility = 'private' with external_only = true
// still reaches Fediverse followers, addressed exactly like a public post,
// despite BroadcastPublicPost's own re-derived visibility check normally
// requiring "public". Requires the local agora-postgres-test instance
// (localhost:15433); skips if it isn't reachable rather than failing the
// suite.
func TestBroadcastPublicPostDeliversExternalOnlyPost(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora.example"}}
	unique := time.Now().UnixNano()

	var authorID string
	username := fmt.Sprintf("agora371_author_%d", unique)
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, profile_private) VALUES ($1, $2, 'x', false)
		RETURNING id
	`, username, username+"@example.com").Scan(&authorID); err != nil {
		t.Fatalf("insert author: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	followerInbox := fmt.Sprintf("https://mastodon.example/users/agora371_%d/inbox", unique)
	if _, err := db.Exec(`
		INSERT INTO ap_followers (followed_user_id, follower_actor_url, follower_inbox_url)
		VALUES ($1, $2, $3)
	`, authorID, fmt.Sprintf("https://mastodon.example/users/agora371_%d", unique), followerInbox); err != nil {
		t.Fatalf("insert follower: %v", err)
	}

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, external_only)
		VALUES ($1, 'only on the fediverse', 'private', true)
		RETURNING id
	`, authorID).Scan(&postID); err != nil {
		t.Fatalf("insert post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })
	t.Cleanup(func() { db.Exec(`DELETE FROM ap_delivery_queue WHERE actor_user_id = $1`, authorID) })

	s.BroadcastPublicPost(authorID, postID)

	var n int
	db.QueryRow(`SELECT COUNT(*) FROM ap_delivery_queue WHERE actor_user_id = $1 AND inbox_url = $2`, authorID, followerInbox).Scan(&n)
	if n == 0 {
		t.Fatalf("expected the external_only post to be queued to the follower's inbox, got none")
	}
}
