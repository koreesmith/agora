package auth

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-356: PublicStats' user_count/post_count had no is_remote filter at
// all, unlike every other count in the codebase — so the public landing
// page reported every cached federated/Bluesky user stub and every
// ingested remote post as if they belonged to this instance. Captures a
// baseline, inserts one local and one remote user (and post), and asserts
// the count increases by exactly 1 (the local one) rather than 2 — proving
// the remote row would have been wrongly counted before this fix.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestPublicStatsExcludesRemoteUsersAndPosts(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}

	getPublicStats := func(t *testing.T) (userCount, postCount int) {
		t.Helper()
		req := httptest.NewRequest("GET", "/stats", nil)
		w := httptest.NewRecorder()
		s.PublicStats(w, req)
		if w.Code != 200 {
			t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		var body struct {
			UserCount int `json:"user_count"`
			PostCount int `json:"post_count"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return body.UserCount, body.PostCount
	}

	baseUsers, basePosts := getPublicStats(t)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var localUserID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, is_remote) VALUES ($1, $2, 'x', false)
		RETURNING id
	`, "agora356local"+suffix, "agora356local"+suffix+"@example.com").Scan(&localUserID); err != nil {
		t.Fatalf("insert local user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, localUserID) })

	var remoteUserID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, is_remote, ap_actor_url)
		VALUES ($1, $2, 'x', true, $3)
		RETURNING id
	`, "agora356remote"+suffix, "agora356remote"+suffix+"@example.com",
		"https://mastodon.example/users/agora356remote"+suffix).Scan(&remoteUserID); err != nil {
		t.Fatalf("insert remote user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, remoteUserID) })

	var localPostID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, is_remote) VALUES ($1, 'local post', 'public', false)
		RETURNING id
	`, localUserID).Scan(&localPostID); err != nil {
		t.Fatalf("insert local post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, localPostID) })

	var remotePostID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, is_remote, remote_post_id, remote_instance)
		VALUES ($1, 'ingested remote post', 'public', true, $2, 'mastodon.example')
		RETURNING id
	`, remoteUserID, "https://mastodon.example/users/agora356remote"+suffix+"/posts/1").Scan(&remotePostID); err != nil {
		t.Fatalf("insert remote post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, remotePostID) })

	gotUsers, gotPosts := getPublicStats(t)

	if gotUsers != baseUsers+1 {
		t.Errorf("user_count went from %d to %d (delta %d), want delta 1 — one local and one remote user were inserted; only the local one should be counted",
			baseUsers, gotUsers, gotUsers-baseUsers)
	}
	if gotPosts != basePosts+1 {
		t.Errorf("post_count went from %d to %d (delta %d), want delta 1 — one local and one remote post were inserted; only the local one should be counted",
			basePosts, gotPosts, gotPosts-basePosts)
	}
}
