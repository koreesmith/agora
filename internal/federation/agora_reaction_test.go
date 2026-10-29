package federation

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-360: isAgoraInstance caches whether a remote domain is confirmed to
// run Agora, so DeliverLike doesn't pay for a live fetch on every reaction.
// Live-fetch itself isn't testable here (fedHTTPClient refuses to dial
// loopback, and there's no real instance at a made-up domain), so this
// seeds the cache directly and checks the read path.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestIsAgoraInstanceServesCachedValue(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}
	domain := fmt.Sprintf("agora-peer-%d.example", time.Now().UnixNano())
	t.Cleanup(func() { db.Exec(`DELETE FROM remote_instance_software WHERE domain = $1`, domain) })

	if _, err := db.Exec(`
		INSERT INTO remote_instance_software (domain, is_agora, checked_at) VALUES ($1, true, NOW())
	`, domain); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	if !s.isAgoraInstance(domain) {
		t.Error("isAgoraInstance = false for a freshly-cached true entry, want true")
	}
}

// AGORA-360: an inbound Like from another Agora instance carries the exact
// reaction chosen in a custom agoraReaction property. handleInboundLike
// must use it (normalized) when present and valid, default to "like" when
// it's absent (matching pre-AGORA-360 behavior for real fediverse Likes),
// and fall back to "like" for a value that doesn't normalize to anything
// recognized rather than storing garbage.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestHandleInboundLikeReactionType(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://test.example"}}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	authorUsername := "agora360author" + suffix

	var authorID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, profile_private, activitypub_enabled)
		VALUES ($1, $2, 'x', false, true)
		RETURNING id
	`, authorUsername, authorUsername+"@example.com").Scan(&authorID); err != nil {
		t.Fatalf("insert author: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility) VALUES ($1, 'agora360 test post', 'public')
		RETURNING id
	`, authorID).Scan(&postID); err != nil {
		t.Fatalf("insert post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	// Pre-seeded as a complete stub (ap_actor_url + a non-empty ap_inbox_url)
	// so getOrCreateRemoteAPUser hits its cache path and never attempts a
	// live actor fetch.
	actorURL := "https://peer.example/federation/users/liker" + suffix
	var remoteUserID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, is_remote, ap_actor_url, ap_inbox_url)
		VALUES ($1, $2, 'x', true, $3, $4)
		RETURNING id
	`, "liker"+suffix+"@peer.example", "liker"+suffix+"@example.com", actorURL, actorURL+"/inbox").Scan(&remoteUserID); err != nil {
		t.Fatalf("insert remote liker stub: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, remoteUserID) })
	t.Cleanup(func() { db.Exec(`DELETE FROM reactions WHERE post_id = $1`, postID) })

	objectURL := "https://test.example/federation/users/" + authorUsername + "/posts/" + postID
	objectRaw, err := json.Marshal(objectURL)
	if err != nil {
		t.Fatalf("marshal objectURL: %v", err)
	}

	getReactionType := func(t *testing.T) string {
		t.Helper()
		var rt string
		db.QueryRow(`SELECT reaction_type FROM reactions WHERE user_id = $1 AND post_id = $2`, remoteUserID, postID).Scan(&rt)
		return rt
	}

	t.Run("no agoraReaction property defaults to like", func(t *testing.T) {
		s.handleInboundLike(actorURL, "", objectRaw, []byte(`{"type":"Like"}`))
		if got := getReactionType(t); got != "like" {
			t.Errorf("reaction_type = %q, want %q", got, "like")
		}
	})

	t.Run("a valid agoraReaction updates the stored type", func(t *testing.T) {
		s.handleInboundLike(actorURL, "", objectRaw, []byte(`{"type":"Like","agoraReaction":"pride"}`))
		if got := getReactionType(t); got != "pride" {
			t.Errorf("reaction_type = %q, want %q", got, "pride")
		}
	})

	t.Run("an unrecognized agoraReaction value falls back to like", func(t *testing.T) {
		// Reset to a third, distinct value first so the assertion below
		// can't be mistaken for the prior test's value surviving untouched.
		if _, err := db.Exec(`UPDATE reactions SET reaction_type = 'wow' WHERE user_id = $1 AND post_id = $2`, remoteUserID, postID); err != nil {
			t.Fatalf("reset reaction_type: %v", err)
		}
		s.handleInboundLike(actorURL, "", objectRaw, []byte(`{"type":"Like","agoraReaction":"not-a-real-reaction"}`))
		if got := getReactionType(t); got != "like" {
			t.Errorf("reaction_type = %q, want %q (fallback for an unrecognized value)", got, "like")
		}
	})
}
