package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-373: edits and deletes of an external_only post must reach the
// Fediverse (they were previously silently swallowed by limitedPostAudience
// treating any 'private'-visibility post, external_only included, as a
// no-audience limited post), and inbound Likes/Announces/replies targeting
// one must resolve rather than being rejected by the 'private' default.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestExternalOnlyPostEditAndDelete(t *testing.T) {
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
	username := fmt.Sprintf("agora373_author_%d", unique)
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, profile_private) VALUES ($1, $2, 'x', false)
		RETURNING id
	`, username, username+"@example.com").Scan(&authorID); err != nil {
		t.Fatalf("insert author: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	followerInbox := fmt.Sprintf("https://mastodon.example/users/agora373_%d/inbox", unique)
	if _, err := db.Exec(`
		INSERT INTO ap_followers (followed_user_id, follower_actor_url, follower_inbox_url)
		VALUES ($1, $2, $3)
	`, authorID, fmt.Sprintf("https://mastodon.example/users/agora373_%d", unique), followerInbox); err != nil {
		t.Fatalf("insert follower: %v", err)
	}

	newPost := func(t *testing.T) string {
		t.Helper()
		var postID string
		if err := db.QueryRow(`
			INSERT INTO posts (author_id, content, visibility, external_only, federate_ap)
			VALUES ($1, 'only out there', 'private', true, true)
			RETURNING id
		`, authorID).Scan(&postID); err != nil {
			t.Fatalf("insert post: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })
		return postID
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM ap_delivery_queue WHERE actor_user_id = $1`, authorID) })

	// Counts newly queued deliveries since a given baseline, rather than an
	// absolute total, since every subtest shares the same author/follower
	// inbox and the queue is only swept once at the very end.
	queuedSince := func(t *testing.T, baseline int) int {
		t.Helper()
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM ap_delivery_queue WHERE actor_user_id = $1 AND inbox_url = $2`, authorID, followerInbox).Scan(&n)
		return n - baseline
	}
	baseline := func(t *testing.T) int {
		t.Helper()
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM ap_delivery_queue WHERE actor_user_id = $1 AND inbox_url = $2`, authorID, followerInbox).Scan(&n)
		return n
	}

	t.Run("edit re-federates to the follower", func(t *testing.T) {
		postID := newPost(t)
		before := baseline(t)
		db.Exec(`UPDATE posts SET content = 'edited, still only out there' WHERE id = $1`, postID)
		s.BroadcastUpdatePost(authorID, postID)
		if queuedSince(t, before) == 0 {
			t.Fatalf("expected the edit to be queued to the follower's inbox, got none")
		}
	})

	t.Run("delete withdraws from the follower", func(t *testing.T) {
		postID := newPost(t)
		before := baseline(t)
		db.Exec(`UPDATE posts SET deleted_at = NOW() WHERE id = $1`, postID)
		s.BroadcastDeletePost(authorID, postID)
		if queuedSince(t, before) == 0 {
			t.Fatalf("expected the delete to be queued to the follower's inbox, got none")
		}
	})

	t.Run("edit and delete stay silent when federate_ap is false", func(t *testing.T) {
		var postID string
		if err := db.QueryRow(`
			INSERT INTO posts (author_id, content, visibility, external_only, federate_ap, federate_atproto)
			VALUES ($1, 'only on bluesky', 'private', true, false, true)
			RETURNING id
		`, authorID).Scan(&postID); err != nil {
			t.Fatalf("insert post: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

		before := baseline(t)
		db.Exec(`UPDATE posts SET content = 'still only on bluesky' WHERE id = $1`, postID)
		s.BroadcastUpdatePost(authorID, postID)
		if n := queuedSince(t, before); n != 0 {
			t.Fatalf("edit: expected nothing queued for a federate_ap:false post, got %d", n)
		}

		db.Exec(`UPDATE posts SET deleted_at = NOW() WHERE id = $1`, postID)
		s.BroadcastDeletePost(authorID, postID)
		if n := queuedSince(t, before); n != 0 {
			t.Fatalf("delete: expected nothing queued for a federate_ap:false post, got %d", n)
		}
	})
}

// AGORA-373: an inbound Like/Announce targeting an external_only post's URI
// must resolve — resolveFederatableTargetFor previously closed the door on
// any 'private'-visibility post, external_only included, per its own
// "everything with no federated audience ... stays closed" default.
func TestResolveFederatableTargetForExternalOnly(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}
	unique := time.Now().UnixNano()
	username := fmt.Sprintf("agora373_target_%d", unique)
	var authorID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, profile_private, activitypub_enabled) VALUES ($1, $2, 'x', false, true)
		RETURNING id
	`, username, username+"@example.com").Scan(&authorID); err != nil {
		t.Fatalf("insert author: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, external_only) VALUES ($1, 'x', 'private', true) RETURNING id
	`, authorID).Scan(&postID); err != nil {
		t.Fatalf("insert post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	objectURL := "https://agora.example/federation/users/" + username + "/posts/" + postID
	s.cfg = &config.Config{InstanceDomain: "https://agora.example"}
	gotPostID, gotAuthorID, ok := s.resolveFederatableTargetFor("https://mastodon.example/users/liker", "https://mastodon.example/users/liker", objectURL)
	if !ok {
		t.Fatalf("expected resolveFederatableTargetFor to accept an external_only post's URI")
	}
	if gotPostID != postID || gotAuthorID != authorID {
		t.Fatalf("got (postID, authorID) = (%q, %q), want (%q, %q)", gotPostID, gotAuthorID, postID, authorID)
	}
}

// AGORA-373: same as above, for the reply-threading resolver used by inbound
// Create(Note).
func TestResolveReplyTargetForExternalOnly(t *testing.T) {
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
	username := fmt.Sprintf("agora373_thread_%d", unique)
	var authorID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id
	`, username, username+"@example.com").Scan(&authorID); err != nil {
		t.Fatalf("insert author: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, external_only) VALUES ($1, 'x', 'private', true) RETURNING id
	`, authorID).Scan(&postID); err != nil {
		t.Fatalf("insert post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	inReplyTo := "https://agora.example/federation/users/" + username + "/posts/" + postID
	_, rootPostID, visibility, gotAuthorID, externalOnly, ok := s.resolveReplyTarget(inReplyTo)
	if !ok {
		t.Fatalf("expected resolveReplyTarget to resolve an external_only post's URI")
	}
	if rootPostID != postID || gotAuthorID != authorID || visibility != "private" || !externalOnly {
		t.Fatalf("got (rootPostID, authorID, visibility, externalOnly) = (%q, %q, %q, %v), want (%q, %q, %q, true)",
			rootPostID, gotAuthorID, visibility, externalOnly, postID, authorID, "private")
	}
}
