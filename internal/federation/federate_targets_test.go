package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-370: a post created with federate_ap: false must never reach a
// single Fediverse follower — neither on creation, nor on a later edit or
// delete. Requires the local agora-postgres-test instance (localhost:15433);
// skips if it isn't reachable rather than failing the suite.
func TestFederateAPFalseSkipsCreateEditAndDelete(t *testing.T) {
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
	username := fmt.Sprintf("agora370_author_%d", unique)
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, profile_private) VALUES ($1, $2, 'x', false)
		RETURNING id
	`, username, username+"@example.com").Scan(&authorID); err != nil {
		t.Fatalf("insert author: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	followerInbox := fmt.Sprintf("https://mastodon.example/users/agora370_%d/inbox", unique)
	if _, err := db.Exec(`
		INSERT INTO ap_followers (followed_user_id, follower_actor_url, follower_inbox_url)
		VALUES ($1, $2, $3)
	`, authorID, fmt.Sprintf("https://mastodon.example/users/agora370_%d", unique), followerInbox); err != nil {
		t.Fatalf("insert follower: %v", err)
	}

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, federate_ap)
		VALUES ($1, 'fediverse says no', 'public', false)
		RETURNING id
	`, authorID).Scan(&postID); err != nil {
		t.Fatalf("insert post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })
	t.Cleanup(func() { db.Exec(`DELETE FROM ap_delivery_queue WHERE actor_user_id = $1`, authorID) })

	assertNothingQueued := func(step string) {
		t.Helper()
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM ap_delivery_queue WHERE actor_user_id = $1 AND inbox_url = $2`, authorID, followerInbox).Scan(&n)
		if n != 0 {
			t.Fatalf("%s: expected nothing queued to the follower's inbox, got %d", step, n)
		}
	}

	s.BroadcastPublicPost(authorID, postID)
	assertNothingQueued("create")

	db.Exec(`UPDATE posts SET content = 'edited' WHERE id = $1`, postID)
	s.BroadcastUpdatePost(authorID, postID)
	assertNothingQueued("edit")

	db.Exec(`UPDATE posts SET deleted_at = NOW() WHERE id = $1`, postID)
	s.BroadcastDeletePost(authorID, postID)
	assertNothingQueued("delete")
}
