package federation

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
	"github.com/go-chi/chi/v5"
)

// AGORA-422: an Agora account's counts as the fediverse reads them. The actor
// document advertised no following collection, so every Agora account showed
// 0 following, and the outbox's totalItems was the size of its 20-post page,
// so every busy account showed at most 20 posts.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable.
func TestActorCollectionCounts(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://test.example"}}
	r := chi.NewRouter()
	RegisterRoutes(r, s)

	username := fmt.Sprintf("agora422_%d", time.Now().UnixNano())
	var userID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, profile_private) VALUES ($1, $2, 'x', false)
		RETURNING id
	`, username, username+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	for i := 0; i < 25; i++ {
		if _, err := db.Exec(`INSERT INTO posts (author_id, content, visibility) VALUES ($1, $2, 'public')`,
			userID, fmt.Sprintf("post %d", i)); err != nil {
			t.Fatalf("insert post: %v", err)
		}
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE author_id = $1`, userID) })

	// One accepted follow and one still-pending request: only the accepted one
	// is a follow.
	for i, accepted := range []bool{true, false} {
		actor := fmt.Sprintf("https://mastodon.example/users/%s_%d", username, i)
		if _, err := db.Exec(`
			INSERT INTO ap_following (follower_user_id, followed_actor_url, followed_inbox_url, accepted)
			VALUES ($1, $2, $3, $4)
		`, userID, actor, actor+"/inbox", accepted); err != nil {
			t.Fatalf("insert ap_following: %v", err)
		}
	}

	get := func(t *testing.T, path string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		// GetUser serves the legacy Agora shape unless the caller asks for ActivityPub.
		req.Header.Set("Accept", "application/activity+json")
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("GET %s = %d; body: %s", path, w.Code, w.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return out
	}

	base := "/federation/users/" + username
	if got := get(t, base+"/following")["totalItems"]; got != float64(1) {
		t.Errorf("following totalItems = %v, want 1 (pending requests are not follows)", got)
	}
	if got := get(t, base+"/outbox")["totalItems"]; got != float64(25) {
		t.Errorf("outbox totalItems = %v, want 25, not the 20-post page size", got)
	}
	if got := get(t, base)["following"]; got != "https://test.example/federation/users/"+username+"/following" {
		t.Errorf("actor document following = %v, want the following collection URL", got)
	}
}
