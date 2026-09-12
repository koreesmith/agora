package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/agora-social/agora/internal/ctxkeys"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-372: the author sees their own external_only post on their own
// profile timeline and via direct GetPost, each carrying an accurate
// "only on ..." badge; no other viewer sees it anywhere.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestExternalOnlyPostVisibility(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var authorID, strangerID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id
	`, "agora372_author_"+suffix, "agora372_author_"+suffix+"@example.com").Scan(&authorID); err != nil {
		t.Fatalf("insert author: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id
	`, "agora372_stranger_"+suffix, "agora372_stranger_"+suffix+"@example.com").Scan(&strangerID); err != nil {
		t.Fatalf("insert stranger: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, strangerID) })

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, external_only, federate_ap, federate_atproto)
		VALUES ($1, 'only out there', 'private', true, true, false)
		RETURNING id
	`, authorID).Scan(&postID); err != nil {
		t.Fatalf("insert post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	s := &Service{db: db}

	getPost := func(viewerID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/posts/"+postID, nil)
		if viewerID != "" {
			req = req.WithContext(context.WithValue(req.Context(), ctxkeys.UserID, viewerID))
		}
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", postID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		s.GetPost(w, req)
		return w
	}

	t.Run("author sees it via GetPost with the fediverse-only badge", func(t *testing.T) {
		w := getPost(authorID)
		if w.Code != 200 {
			t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Post Post `json:"post"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !resp.Post.ExternalOnly || resp.Post.OnlyOn != "Only on the Fediverse" {
			t.Fatalf("got ExternalOnly=%v OnlyOn=%q, want true / %q", resp.Post.ExternalOnly, resp.Post.OnlyOn, "Only on the Fediverse")
		}
	})

	t.Run("a stranger is denied via GetPost", func(t *testing.T) {
		w := getPost(strangerID)
		if w.Code != 403 {
			t.Fatalf("status = %d, want 403; body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("a logged-out viewer is denied via GetPost", func(t *testing.T) {
		w := getPost("")
		if w.Code != 403 {
			t.Fatalf("status = %d, want 403; body: %s", w.Code, w.Body.String())
		}
	})

	getUserPosts := func(viewerID, username string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/users/"+username+"/posts", nil)
		if viewerID != "" {
			req = req.WithContext(context.WithValue(req.Context(), ctxkeys.UserID, viewerID))
		}
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", username)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		s.GetUserPosts(w, req)
		return w
	}

	t.Run("author sees it on their own timeline with the badge", func(t *testing.T) {
		w := getUserPosts(authorID, "agora372_author_"+suffix)
		if w.Code != 200 {
			t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Posts []Post `json:"posts"`
		}
		json.Unmarshal(w.Body.Bytes(), &resp)
		found := false
		for _, p := range resp.Posts {
			if p.ID == postID {
				found = true
				if !p.ExternalOnly || p.OnlyOn != "Only on the Fediverse" {
					t.Fatalf("got ExternalOnly=%v OnlyOn=%q, want true / %q", p.ExternalOnly, p.OnlyOn, "Only on the Fediverse")
				}
			}
		}
		if !found {
			t.Fatalf("expected the external_only post on the author's own timeline, got %d post(s)", len(resp.Posts))
		}
	})

	t.Run("a stranger never sees it on the author's timeline", func(t *testing.T) {
		w := getUserPosts(strangerID, "agora372_author_"+suffix)
		if w.Code != 200 {
			t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Posts []Post `json:"posts"`
		}
		json.Unmarshal(w.Body.Bytes(), &resp)
		for _, p := range resp.Posts {
			if p.ID == postID {
				t.Fatalf("expected the external_only post to be absent from a stranger's view of the timeline")
			}
		}
	})
}
