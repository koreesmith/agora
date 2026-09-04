package federation

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-357: reposting ("sharing") a post whose original author has no
// ap_actor_url — a purely local Agora post, or a Bluesky-origin one — used
// to never reach the reposter's own fediverse followers at all.
// DeliverAnnounce required lookupRemoteTarget to succeed *before* it would
// call deliverToFollowers, so it returned early for exactly those cases.
// This reproduces that scenario directly against a real DB: a local
// original post, a reposter with a real fediverse follower, and asserts a
// delivery-queue row now shows up for that follower's inbox (it wouldn't
// have, pre-fix).
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestDeliverAnnounceFansOutForLocalOriginal(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://test.example"}}

	mkUser := func(t *testing.T) string {
		t.Helper()
		username := fmt.Sprintf("agora357_%d", time.Now().UnixNano())
		var id string
		if err := db.QueryRow(`
			INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')
			RETURNING id
		`, username, username+"@example.com").Scan(&id); err != nil {
			t.Fatalf("insert test user: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}

	origAuthor := mkUser(t)
	reposter := mkUser(t)

	var origPostID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility) VALUES ($1, 'the original post', 'public')
		RETURNING id
	`, origAuthor).Scan(&origPostID); err != nil {
		t.Fatalf("insert original post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, origPostID) })

	var repostID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, repost_of_id) VALUES ($1, '', 'public', $2)
		RETURNING id
	`, reposter, origPostID).Scan(&repostID); err != nil {
		t.Fatalf("insert repost: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, repostID) })

	followerInbox := fmt.Sprintf("https://mastodon.example/users/follower-%d/inbox", time.Now().UnixNano())
	if _, err := db.Exec(`
		INSERT INTO ap_followers (followed_user_id, follower_actor_url, follower_inbox_url)
		VALUES ($1, $2, $3)
	`, reposter, followerInbox+"#actor", followerInbox); err != nil {
		t.Fatalf("insert follower: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM ap_followers WHERE followed_user_id = $1`, reposter) })
	t.Cleanup(func() { db.Exec(`DELETE FROM ap_delivery_queue WHERE actor_user_id = $1`, reposter) })

	s.DeliverAnnounce(reposter, repostID, origPostID)

	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM ap_delivery_queue WHERE actor_user_id = $1 AND inbox_url = $2
	`, reposter, followerInbox).Scan(&count); err != nil {
		t.Fatalf("count delivery queue rows: %v", err)
	}
	if count != 1 {
		t.Errorf("ap_delivery_queue has %d row(s) for the reposter's follower after announcing a repost of a LOCAL post, want 1 (the Announce should always fan out to followers regardless of the original's origin)", count)
	}
}

// AGORA-357: a post's own ActivityPub object URL
// (.../federation/users/{handle}/posts/{postID}) never had a route serving
// it, so any remote server dereferencing an object by its own id — e.g.
// Mastodon rendering a boosted post — got a 404. Exercises the real route
// end-to-end (chi router + GetPost handler), not just the underlying query.
func TestGetPostRoute(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://test.example"}}
	r := chi.NewRouter()
	RegisterRoutes(r, s)

	username := fmt.Sprintf("agora357b_%d", time.Now().UnixNano())
	var authorID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, profile_private) VALUES ($1, $2, 'x', false)
		RETURNING id
	`, username, username+"@example.com").Scan(&authorID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	var publicPostID, privatePostID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility) VALUES ($1, 'hello fediverse', 'public') RETURNING id
	`, authorID).Scan(&publicPostID); err != nil {
		t.Fatalf("insert public post: %v", err)
	}
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility) VALUES ($1, 'shh', 'private') RETURNING id
	`, authorID).Scan(&privatePostID); err != nil {
		t.Fatalf("insert private post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE author_id = $1`, authorID) })

	t.Run("public post serves a dereferenceable Note", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/federation/users/"+username+"/posts/"+publicPostID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/activity+json" {
			t.Errorf("Content-Type = %q, want application/activity+json", ct)
		}
		var note map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &note); err != nil {
			t.Fatalf("response body did not decode as JSON: %v", err)
		}
		wantID := "https://test.example/federation/users/" + username + "/posts/" + publicPostID
		if note["id"] != wantID {
			t.Errorf("note[id] = %v, want %q", note["id"], wantID)
		}
		if note["type"] != "Note" {
			t.Errorf("note[type] = %v, want Note", note["type"])
		}
		if note["@context"] != "https://www.w3.org/ns/activitystreams" {
			t.Errorf("note[@context] = %v, want the activitystreams context (must be set on the bare object when served standalone, unlike inside a Create wrapper)", note["@context"])
		}
	})

	t.Run("private post 404s rather than leaking content", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/federation/users/"+username+"/posts/"+privatePostID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != 404 {
			t.Errorf("status = %d, want 404 for a private post's object URL; body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("nonexistent post 404s", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/federation/users/"+username+"/posts/00000000-0000-0000-0000-000000000000", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != 404 {
			t.Errorf("status = %d, want 404 for a nonexistent post id", w.Code)
		}
	})
}
