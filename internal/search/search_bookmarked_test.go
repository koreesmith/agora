package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/ctxkeys"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-434: a post the viewer bookmarked must say so in post search results.
// Needs the local agora-postgres-test instance; skips otherwise.
func TestSearchPostsReportsBookmarked(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	n := time.Now().UnixNano()
	user := func(name string) string {
		var id string
		if err := db.QueryRow(`
			INSERT INTO users (username, email, password_hash, email_verified, profile_private)
			VALUES ($1, $2, 'x', true, false) RETURNING id
		`, fmt.Sprintf("agora434%s%d", name, n), fmt.Sprintf("agora434%s%d@x.invalid", name, n)).Scan(&id); err != nil {
			t.Fatalf("insert user: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	post := func(author, content string) string {
		var id string
		if err := db.QueryRow(`INSERT INTO posts (author_id, content, visibility) VALUES ($1, $2, 'public') RETURNING id`,
			author, content).Scan(&id); err != nil {
			t.Fatalf("insert post: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, id) })
		return id
	}

	viewer, author := user("viewer"), user("author")
	saved := post(author, fmt.Sprintf("agora434needle saved %d", n))
	unsaved := post(author, fmt.Sprintf("agora434needle unsaved %d", n))
	db.Exec(`INSERT INTO bookmarks (user_id, post_id) VALUES ($1, $2)`, viewer, saved)

	search := func(as string) map[string]bool {
		req := httptest.NewRequest("GET", "/search/posts?q=agora434needle", nil).
			WithContext(context.WithValue(context.Background(), ctxkeys.UserID, as))
		w := httptest.NewRecorder()
		NewService(db).SearchPosts(w, req)
		var body struct {
			Posts []struct {
				ID         string `json:"id"`
				Bookmarked bool   `json:"bookmarked"`
			} `json:"posts"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v (%s)", err, w.Body.String())
		}
		got := map[string]bool{}
		for _, p := range body.Posts {
			got[p.ID] = p.Bookmarked
		}
		return got
	}

	got := search(viewer)
	if len(got) != 2 {
		t.Fatalf("want both posts in results, got %v", got)
	}
	if !got[saved] || got[unsaved] {
		t.Errorf("bookmarked flags wrong for the viewer: saved=%v unsaved=%v", got[saved], got[unsaved])
	}
	if other := search(author); other[saved] {
		t.Error("another user's bookmark leaked into the author's results")
	}
}
