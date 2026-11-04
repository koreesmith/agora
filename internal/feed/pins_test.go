package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/agora-social/agora/internal/ctxkeys"
	"github.com/agora-social/agora/internal/store"
)

type pinEnv struct {
	t  *testing.T
	db *store.DB
	s  *Service
}

func newPinEnv(t *testing.T) *pinEnv {
	db := openVideoMetaDB(t)
	return &pinEnv{t: t, db: db, s: &Service{db: db}}
}

func (e *pinEnv) user(tag string) (id, username string) {
	username = fmt.Sprintf("pin447_%s_%d", tag, time.Now().UnixNano())
	if err := e.db.QueryRow(`INSERT INTO users (username, email, password_hash, email_verified, profile_private) VALUES ($1, $2, 'x', true, false) RETURNING id`,
		username, username+"@x.invalid").Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { e.db.Exec(`DELETE FROM users WHERE id = $1`, id) })
	return
}

func (e *pinEnv) post(author, visibility, content string, minutesAgo int) string {
	var id string
	if err := e.db.QueryRow(`INSERT INTO posts (author_id, content, visibility, published_at, created_at)
		VALUES ($1, $2, $3, NOW() - ($4 * INTERVAL '1 minute'), NOW() - ($4 * INTERVAL '1 minute')) RETURNING id`,
		author, content, visibility, minutesAgo).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { e.db.Exec(`DELETE FROM posts WHERE id = $1`, id) })
	return id
}

// do runs a handler as userID ("" is a guest) with the given route parameters.
func (e *pinEnv) do(h func(http.ResponseWriter, *http.Request), userID, method, target string, params map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if userID != "" {
		ctx = context.WithValue(ctx, ctxkeys.UserID, userID)
	}
	w := httptest.NewRecorder()
	h(w, req.WithContext(ctx))
	return w
}

func (e *pinEnv) pin(userID, postID string) int {
	return e.do(e.s.PinPost, userID, "POST", "/posts/"+postID+"/pin", map[string]string{"id": postID}).Code
}
func (e *pinEnv) unpin(userID, postID string) int {
	return e.do(e.s.UnpinPost, userID, "DELETE", "/posts/"+postID+"/pin", map[string]string{"id": postID}).Code
}

type profileResp struct {
	Posts       []Post `json:"posts"`
	PinnedPosts []Post `json:"pinned_posts"`
}

func (e *pinEnv) profile(viewer, username string, offset int) (int, profileResp, map[string]json.RawMessage) {
	w := e.do(e.s.GetUserPosts, viewer, "GET", fmt.Sprintf("/users/%s/posts?offset=%d", username, offset), map[string]string{"username": username})
	var out profileResp
	json.Unmarshal(w.Body.Bytes(), &out)
	var raw map[string]json.RawMessage
	json.Unmarshal(w.Body.Bytes(), &raw)
	return w.Code, out, raw
}

func ids(ps []Post) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

// Pinning works for your own posts, is idempotent, stops at the limit, and
// frees a slot when a pin is removed.
func TestPinLimitAndIdempotence(t *testing.T) {
	e := newPinEnv(t)
	me, _ := e.user("owner")
	p := []string{}
	for i := 1; i <= 5; i++ {
		p = append(p, e.post(me, "public", fmt.Sprintf("post %d", i), i))
	}

	for i := 0; i < MaxPinnedPosts; i++ {
		if code := e.pin(me, p[i]); code != 200 {
			t.Fatalf("pin %d = %d", i+1, code)
		}
	}
	if code := e.pin(me, p[0]); code != 200 {
		t.Errorf("pinning an already pinned post = %d, want 200 (idempotent)", code)
	}
	w := e.do(e.s.PinPost, me, "POST", "/posts/x/pin", map[string]string{"id": p[3]})
	if w.Code != 409 {
		t.Fatalf("a fourth pin = %d, want 409", w.Code)
	}
	var msg struct{ Error string }
	json.Unmarshal(w.Body.Bytes(), &msg)
	if msg.Error == "" {
		t.Error("the refusal has no message")
	}

	if code := e.unpin(me, p[1]); code != 200 {
		t.Fatalf("unpin = %d", code)
	}
	if code := e.unpin(me, p[1]); code != 200 {
		t.Errorf("unpinning what isn't pinned = %d, want 200", code)
	}
	if code := e.pin(me, p[3]); code != 200 {
		t.Errorf("after unpinning, a new pin = %d, want 200", code)
	}
}

// Only your own top-level posts on your profile can be pinned.
func TestPinOnlyOwnTopLevelPosts(t *testing.T) {
	e := newPinEnv(t)
	me, _ := e.user("me")
	other, _ := e.user("other")

	mine := e.post(me, "public", "mine", 1)
	theirs := e.post(other, "public", "theirs", 1)
	if code := e.pin(me, theirs); code != 403 {
		t.Errorf("someone else's post = %d, want 403", code)
	}
	if code := e.pin(me, "00000000-0000-0000-0000-000000000000"); code != 404 {
		t.Errorf("a missing post = %d, want 404", code)
	}

	var comment, repost, wall string
	e.db.QueryRow(`INSERT INTO posts (author_id, content, visibility, parent_id) VALUES ($1, 'c', 'public', $2) RETURNING id`, me, theirs).Scan(&comment)
	e.db.QueryRow(`INSERT INTO posts (author_id, content, visibility, repost_of_id) VALUES ($1, 'r', 'public', $2) RETURNING id`, me, theirs).Scan(&repost)
	e.db.QueryRow(`INSERT INTO posts (author_id, content, visibility, wall_user_id, wall_status) VALUES ($1, 'w', 'public', $2, 'approved') RETURNING id`, me, other).Scan(&wall)
	t.Cleanup(func() { e.db.Exec(`DELETE FROM posts WHERE id IN ($1, $2, $3)`, comment, repost, wall) })
	for name, id := range map[string]string{"a comment": comment, "a repost": repost, "a post on someone's wall": wall} {
		if code := e.pin(me, id); code != 400 {
			t.Errorf("%s = %d, want 400", name, code)
		}
	}
	if code := e.pin(me, mine); code != 200 {
		t.Errorf("your own post = %d, want 200", code)
	}

	// a deleted post can't be pinned, and its pin goes with it
	if w := e.do(e.s.DeletePost, me, "DELETE", "/posts/"+mine, map[string]string{"id": mine}); w.Code != 200 {
		t.Fatalf("delete = %d", w.Code)
	}
	var n int
	e.db.QueryRow(`SELECT COUNT(*) FROM pinned_posts WHERE post_id = $1`, mine).Scan(&n)
	if n != 0 {
		t.Error("deleting a pinned post left its pin behind")
	}
	if code := e.pin(me, mine); code != 404 {
		t.Errorf("pinning a deleted post = %d, want 404", code)
	}
}

// Deleting a pinned post frees its slot.
func TestDeletingAPinnedPostFreesTheSlot(t *testing.T) {
	e := newPinEnv(t)
	me, _ := e.user("slot")
	var ps []string
	for i := 1; i <= 4; i++ {
		ps = append(ps, e.post(me, "public", fmt.Sprintf("p%d", i), i))
	}
	for i := 0; i < MaxPinnedPosts; i++ {
		e.pin(me, ps[i])
	}
	if code := e.pin(me, ps[3]); code != 409 {
		t.Fatalf("at the limit = %d, want 409", code)
	}
	e.do(e.s.DeletePost, me, "DELETE", "/posts/x", map[string]string{"id": ps[0]})
	if code := e.pin(me, ps[3]); code != 200 {
		t.Errorf("after deleting a pinned post the slot should be free, got %d", code)
	}
}

// The first page of a profile carries the pins, newest pin first, in addition to
// the usual page; later pages don't repeat them; the page itself is unchanged.
func TestProfileLeadsWithPinnedPosts(t *testing.T) {
	e := newPinEnv(t)
	me, name := e.user("profile")
	old := e.post(me, "public", "old", 30)
	mid := e.post(me, "public", "mid", 20)
	newest := e.post(me, "public", "newest", 1)
	e.pin(me, old)
	time.Sleep(15 * time.Millisecond)
	e.pin(me, mid)

	code, first, raw := e.profile(me, name, 0)
	if code != 200 {
		t.Fatalf("profile = %d", code)
	}
	if got := ids(first.PinnedPosts); fmt.Sprint(got) != fmt.Sprint([]string{mid, old}) {
		t.Errorf("pinned = %v, want the newest pin first: %v", got, []string{mid, old})
	}
	for _, p := range first.PinnedPosts {
		if !p.Pinned {
			t.Errorf("pinned post %s is not flagged pinned", p.ID)
		}
	}
	// the page is as it always was: every post, in order, with the pinned ones marked
	if got := ids(first.Posts); fmt.Sprint(got) != fmt.Sprint([]string{newest, mid, old}) {
		t.Errorf("posts = %v, want all three in date order", got)
	}
	flags := map[string]bool{}
	for _, p := range first.Posts {
		flags[p.ID] = p.Pinned
	}
	if flags[newest] || !flags[mid] || !flags[old] {
		t.Errorf("pinned flags on the page: %v", flags)
	}

	_, _, later := e.profile(me, name, 20)
	if _, has := later["pinned_posts"]; has {
		t.Error("a later page repeated the pinned posts")
	}
	_ = raw
}

// A pin follows the post's own audience: a viewer who can't see the post gets no
// trace of it, and a blocked or private-profile viewer gets nothing extra.
func TestPinnedPostsFollowTheAudience(t *testing.T) {
	e := newPinEnv(t)
	me, name := e.user("aud")
	friend, _ := e.user("friend")
	stranger, _ := e.user("stranger")
	blocked, _ := e.user("blocked")
	e.db.Exec(`INSERT INTO friendships (requester_id, addressee_id, status) VALUES ($1, $2, 'accepted')`, me, friend)
	e.db.Exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, me, blocked)
	t.Cleanup(func() { e.db.Exec(`DELETE FROM blocks WHERE blocker_id = $1`, me) })

	pub := e.post(me, "public", "public one", 5)
	friendsOnly := e.post(me, "friends", "friends only", 10)
	e.pin(me, pub)
	time.Sleep(15 * time.Millisecond)
	e.pin(me, friendsOnly)

	pinnedFor := func(viewer string) []string {
		_, r, _ := e.profile(viewer, name, 0)
		return ids(r.PinnedPosts)
	}
	if got := pinnedFor(me); fmt.Sprint(got) != fmt.Sprint([]string{friendsOnly, pub}) {
		t.Errorf("the author sees %v", got)
	}
	if got := pinnedFor(friend); fmt.Sprint(got) != fmt.Sprint([]string{friendsOnly, pub}) {
		t.Errorf("a friend sees %v", got)
	}
	if got := pinnedFor(stranger); fmt.Sprint(got) != fmt.Sprint([]string{pub}) {
		t.Errorf("a stranger sees %v, want only the public one and no gap", got)
	}
	if got := pinnedFor(""); fmt.Sprint(got) != fmt.Sprint([]string{pub}) {
		t.Errorf("a guest sees %v, want only the public one", got)
	}
	if got := pinnedFor(blocked); len(got) != 0 {
		t.Errorf("a blocked viewer sees %v", got)
	}

	// a private profile shows non-friends nothing, pins included
	e.db.Exec(`UPDATE users SET profile_private = true WHERE id = $1`, me)
	if got := pinnedFor(stranger); len(got) != 0 {
		t.Errorf("a stranger on a private profile sees %v", got)
	}
	if got := pinnedFor(friend); len(got) != 2 {
		t.Errorf("a friend on a private profile sees %v, want both", got)
	}
}

// Changing a pinned post's audience doesn't unpin it; it just stops showing to
// people who can no longer see it.
func TestChangingAudienceKeepsThePin(t *testing.T) {
	e := newPinEnv(t)
	me, name := e.user("chg")
	stranger, _ := e.user("chgstranger")
	p := e.post(me, "public", "was public", 1)
	e.pin(me, p)
	e.db.Exec(`UPDATE posts SET visibility = 'friends' WHERE id = $1`, p)

	_, r, _ := e.profile(stranger, name, 0)
	if len(r.PinnedPosts) != 0 {
		t.Errorf("a stranger still sees the pin after it went friends-only: %v", ids(r.PinnedPosts))
	}
	_, r, _ = e.profile(me, name, 0)
	if len(r.PinnedPosts) != 1 {
		t.Errorf("the author lost the pin when changing its audience: %v", ids(r.PinnedPosts))
	}
}

// Any list of posts carries the pinned flag, so the menu can offer Pin or Unpin.
func TestMarkPinnedFlagsPostsInAnyList(t *testing.T) {
	e := newPinEnv(t)
	me, _ := e.user("flag")
	a := e.post(me, "public", "a", 2)
	b := e.post(me, "public", "b", 1)
	e.pin(me, a)
	posts := []Post{{ID: a}, {ID: b}}
	e.s.markPinned(posts)
	if !posts[0].Pinned || posts[1].Pinned {
		t.Errorf("flags: %v %v", posts[0].Pinned, posts[1].Pinned)
	}
	e.s.markPinned(nil) // an empty page must not query or panic
}
