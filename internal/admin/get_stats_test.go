package admin

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-356: postsToday/activeUsers7d in GetStats had no is_remote filter,
// so a followed fediverse/Bluesky account's ingested posts inflated both —
// "Active (7d)" in particular counted a remote author whose post got
// ingested that week as if they were an active local user. Captures a
// baseline, inserts one local post (by a fresh local author) and one
// remote-ingested post (by a fresh remote author), and asserts both stats
// increase by exactly 1, not 2.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestGetStatsExcludesRemotePosts(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}

	getStats := func(t *testing.T) (postsToday, activeUsers7d int) {
		t.Helper()
		req := httptest.NewRequest("GET", "/admin/stats", nil)
		w := httptest.NewRecorder()
		s.GetStats(w, req)
		if w.Code != 200 {
			t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		var body struct {
			PostsToday    int `json:"posts_today"`
			ActiveUsers7d int `json:"active_users_7d"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return body.PostsToday, body.ActiveUsers7d
	}

	basePostsToday, baseActive7d := getStats(t)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var localUserID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, is_remote) VALUES ($1, $2, 'x', false)
		RETURNING id
	`, "agora356admlocal"+suffix, "agora356admlocal"+suffix+"@example.com").Scan(&localUserID); err != nil {
		t.Fatalf("insert local user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, localUserID) })

	var remoteUserID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, is_remote, ap_actor_url)
		VALUES ($1, $2, 'x', true, $3)
		RETURNING id
	`, "agora356admremote"+suffix, "agora356admremote"+suffix+"@example.com",
		"https://mastodon.example/users/agora356admremote"+suffix).Scan(&remoteUserID); err != nil {
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
	`, remoteUserID, "https://mastodon.example/users/agora356admremote"+suffix+"/posts/1").Scan(&remotePostID); err != nil {
		t.Fatalf("insert remote post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, remotePostID) })

	gotPostsToday, gotActive7d := getStats(t)

	if gotPostsToday != basePostsToday+1 {
		t.Errorf("posts_today went from %d to %d (delta %d), want delta 1 — the remote-ingested post must not be counted",
			basePostsToday, gotPostsToday, gotPostsToday-basePostsToday)
	}
	if gotActive7d != baseActive7d+1 {
		t.Errorf("active_users_7d went from %d to %d (delta %d), want delta 1 — the remote author must not count as an active local user",
			baseActive7d, gotActive7d, gotActive7d-baseActive7d)
	}
}
