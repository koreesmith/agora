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

// AGORA-429. Requires the local agora-postgres-test instance; skips otherwise.
func TestBookmarks(t *testing.T) {
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
		`, fmt.Sprintf("agora429%s%d", name, n), fmt.Sprintf("agora429%s%d@x.invalid", name, n)).Scan(&id); err != nil {
			t.Fatalf("insert user %s: %v", name, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	post := func(author, visibility, content string, parent *string) string {
		var id string
		if err := db.QueryRow(`
			INSERT INTO posts (author_id, content, visibility, parent_id) VALUES ($1, $2, $3, $4) RETURNING id
		`, author, content, visibility, parent).Scan(&id); err != nil {
			t.Fatalf("insert post: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, id) })
		return id
	}

	viewer, friend, stranger, other := user("viewer"), user("friend"), user("stranger"), user("other")
	db.Exec(`INSERT INTO friendships (requester_id, addressee_id, status) VALUES ($1, $2, 'accepted')`, viewer, friend)

	publicPost := post(stranger, "public", "public from a stranger", nil)
	friendsPost := post(friend, "friends", "friends only, from a friend", nil)
	strangersFriendsPost := post(stranger, "friends", "friends only, from a stranger", nil)
	ownPost := post(viewer, "private", "my own", nil)
	comment := post(stranger, "public", "a comment", &publicPost)
	deletedPost := post(stranger, "public", "soon deleted", nil)

	s := &Service{db: db}
	add := func(viewerID, postID string) int {
		req := httptest.NewRequest("POST", "/posts/"+postID+"/bookmark", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", postID)
		req = req.WithContext(context.WithValue(context.WithValue(req.Context(), ctxkeys.UserID, viewerID), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		s.AddBookmark(w, req)
		return w.Code
	}
	remove := func(viewerID, postID string) int {
		req := httptest.NewRequest("DELETE", "/posts/"+postID+"/bookmark", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", postID)
		req = req.WithContext(context.WithValue(context.WithValue(req.Context(), ctxkeys.UserID, viewerID), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		s.RemoveBookmark(w, req)
		return w.Code
	}
	list := func(viewerID, query string) []Post {
		req := httptest.NewRequest("GET", "/bookmarks"+query, nil).
			WithContext(context.WithValue(context.Background(), ctxkeys.UserID, viewerID))
		w := httptest.NewRecorder()
		s.ListBookmarks(w, req)
		if w.Code != 200 {
			t.Fatalf("list status = %d; body: %s", w.Code, w.Body.String())
		}
		var body struct {
			Posts []Post `json:"posts"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body.Posts
	}
	ids := func(posts []Post) []string {
		out := make([]string, len(posts))
		for i, p := range posts {
			out[i] = p.ID
		}
		return out
	}

	// What can and cannot be saved.
	if got := add(viewer, publicPost); got != 200 {
		t.Errorf("bookmarking a public post: status %d, want 200", got)
	}
	if got := add(viewer, publicPost); got != 200 {
		t.Errorf("bookmarking twice: status %d, want 200 (idempotent)", got)
	}
	time.Sleep(10 * time.Millisecond)
	if got := add(viewer, friendsPost); got != 200 {
		t.Errorf("bookmarking a friend's friends-only post: status %d, want 200", got)
	}
	time.Sleep(10 * time.Millisecond)
	if got := add(viewer, ownPost); got != 200 {
		t.Errorf("bookmarking your own post: status %d, want 200", got)
	}
	if got := add(viewer, strangersFriendsPost); got != 404 {
		t.Errorf("bookmarking a friends-only post you cannot see: status %d, want 404", got)
	}
	if got := add(viewer, comment); got != 404 {
		t.Errorf("bookmarking a comment: status %d, want 404", got)
	}
	if got := add(viewer, "00000000-0000-0000-0000-000000000000"); got != 404 {
		t.Errorf("bookmarking a missing post: status %d, want 404", got)
	}

	// Newest saved first, each flagged as bookmarked.
	got := list(viewer, "")
	want := []string{ownPost, friendsPost, publicPost}
	if fmt.Sprint(ids(got)) != fmt.Sprint(want) {
		t.Fatalf("list = %v, want %v", ids(got), want)
	}
	for _, p := range got {
		if !p.Bookmarked {
			t.Errorf("post %s in the list is not flagged bookmarked", p.ID)
		}
	}

	// Paging is by the saved order.
	if page := list(viewer, "?limit=1&offset=1"); len(page) != 1 || page[0].ID != friendsPost {
		t.Errorf("second page = %v, want [%s]", ids(page), friendsPost)
	}

	// Bookmarks are private to the user.
	if l := list(other, ""); len(l) != 0 {
		t.Errorf("another user sees %d bookmarks, want 0", len(l))
	}

	// A deleted post and a blocked author drop out quietly, and come back when
	// the block goes.
	db.Exec(`UPDATE posts SET deleted_at = NOW() WHERE id = $1`, deletedPost)
	db.Exec(`INSERT INTO bookmarks (user_id, post_id) VALUES ($1, $2)`, viewer, deletedPost)
	if l := list(viewer, ""); fmt.Sprint(ids(l)) != fmt.Sprint(want) {
		t.Errorf("list with a deleted bookmark = %v, want %v", ids(l), want)
	}
	db.Exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, viewer, stranger)
	t.Cleanup(func() { db.Exec(`DELETE FROM blocks WHERE blocker_id = $1`, viewer) })
	if l := list(viewer, ""); fmt.Sprint(ids(l)) != fmt.Sprint([]string{ownPost, friendsPost}) {
		t.Errorf("list with a blocked author = %v, want own and friend's posts", ids(l))
	}
	db.Exec(`DELETE FROM blocks WHERE blocker_id = $1`, viewer)

	// Losing access (unfriending) hides the post without an error.
	db.Exec(`DELETE FROM friendships WHERE requester_id = $1`, viewer)
	if l := list(viewer, ""); fmt.Sprint(ids(l)) != fmt.Sprint([]string{ownPost, publicPost}) {
		t.Errorf("list after unfriending = %v, want own and public posts", ids(l))
	}

	// Removing, and removing again.
	if got := remove(viewer, publicPost); got != 200 {
		t.Errorf("remove: status %d, want 200", got)
	}
	if got := remove(viewer, publicPost); got != 200 {
		t.Errorf("remove twice: status %d, want 200", got)
	}
	if l := list(viewer, ""); fmt.Sprint(ids(l)) != fmt.Sprint([]string{ownPost}) {
		t.Errorf("list after remove = %v, want only own post", ids(l))
	}
}
