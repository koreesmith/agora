package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/ctxkeys"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-400: image descriptions are stored per image when a post is created,
// come back in photo_alts lined up with photo_urls, and can be rewritten on
// their own by an edit, without re-uploading the images.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestImageAlts(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil { t.Skipf("skipping: agora-postgres-test not reachable: %v", err) }
	t.Cleanup(func() { db.Close() }) // registered first, so it runs last
	if err := db.Migrate(); err != nil { t.Fatalf("migrate: %v", err) }

	var userID string
	name := fmt.Sprintf("alt400_%d", time.Now().UnixNano())
	if err := db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1, $1, 'x') RETURNING id`, name).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	// Private, so nothing tries to federate.
	s := &Service{db: db}
	create := func(body string) string {
		t.Helper()
		req := httptest.NewRequest("POST", "/posts", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), ctxkeys.UserID, userID))
		w := httptest.NewRecorder()
		s.CreatePost(w, req)
		if w.Code != 201 { t.Fatalf("create: status %d: %s", w.Code, w.Body.String()) }
		var resp struct{ ID string `json:"id"` }
		json.Unmarshal(w.Body.Bytes(), &resp)
		return resp.ID
	}
	edit := func(postID, body string) {
		t.Helper()
		req := httptest.NewRequest("PATCH", "/posts/"+postID, strings.NewReader(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", postID)
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(context.WithValue(ctx, ctxkeys.UserID, userID))
		w := httptest.NewRecorder()
		s.EditPost(w, req)
		if w.Code != 200 { t.Fatalf("edit: status %d: %s", w.Code, w.Body.String()) }
	}
	alts := func(postID string) []string {
		t.Helper()
		var imageURL string
		db.QueryRow(`SELECT image_url FROM posts WHERE id = $1`, postID).Scan(&imageURL)
		posts := []Post{{ID: postID, ImageURL: imageURL}}
		s.enrichPhotos(posts)
		return posts[0].PhotoAlts
	}

	multi := create(`{"content":"two photos","visibility":"private",
		"image_urls":["/uploads/posts/a.jpg","/uploads/posts/b.jpg"],
		"image_alts":["  A dog on a beach  ",""]}`)
	if got := alts(multi); fmt.Sprint(got) != fmt.Sprint([]string{"A dog on a beach", ""}) {
		t.Errorf("multi-photo photo_alts = %q, want the trimmed first and an empty second", got)
	}

	single := create(`{"content":"one photo","visibility":"private","image_url":"/uploads/posts/c.jpg","image_alt":"A sunset"}`)
	if got := alts(single); fmt.Sprint(got) != fmt.Sprint([]string{"A sunset"}) {
		t.Errorf("single-image photo_alts = %q, want [A sunset]", got)
	}

	none := create(`{"content":"no description","visibility":"private","image_url":"/uploads/posts/d.jpg"}`)
	if got := alts(none); got != nil {
		t.Errorf("photo_alts without descriptions = %q, want it left out", got)
	}

	t.Run("an edit can rewrite descriptions alone", func(t *testing.T) {
		edit(multi, `{"image_alts":["A dog","A cat"]}`)
		if got := alts(multi); fmt.Sprint(got) != fmt.Sprint([]string{"A dog", "A cat"}) {
			t.Errorf("after edit photo_alts = %q, want [A dog A cat]", got)
		}
		var urls int
		db.QueryRow(`SELECT COUNT(*) FROM post_photos WHERE post_id = $1`, multi).Scan(&urls)
		if urls != 2 { t.Errorf("the photos themselves changed: %d rows, want 2", urls) }

		edit(single, `{"image_alts":["A red sunset"]}`)
		if got := alts(single); fmt.Sprint(got) != fmt.Sprint([]string{"A red sunset"}) {
			t.Errorf("single after edit photo_alts = %q", got)
		}
	})

	t.Run("an edit that replaces the images takes their new descriptions", func(t *testing.T) {
		edit(multi, `{"image_urls":["/uploads/posts/e.jpg","/uploads/posts/f.jpg","/uploads/posts/g.jpg"],"image_alts":["","","Third"]}`)
		if got := alts(multi); fmt.Sprint(got) != fmt.Sprint([]string{"", "", "Third"}) {
			t.Errorf("after replacing images photo_alts = %q", got)
		}
	})
}
