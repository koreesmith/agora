package feed

import (
	"bytes"
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

type httptestReq struct {
	r      *http.Request
	h      http.HandlerFunc
	params map[string]string
}
type httptestRec = httptest.ResponseRecorder

// AGORA-428. Requires the local agora-postgres-test instance; skips otherwise.
func TestKeywordMutes(t *testing.T) {
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
		`, fmt.Sprintf("agora428%s%d", name, n), fmt.Sprintf("agora428%s%d@x.invalid", name, n)).Scan(&id); err != nil {
			t.Fatalf("insert user %s: %v", name, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	post := func(author, content, cw, alt string, repostOf *string) string {
		var id string
		if err := db.QueryRow(`
			INSERT INTO posts (author_id, content, visibility, content_warning, image_alt, repost_of_id)
			VALUES ($1, $2, 'public', $3, $4, $5) RETURNING id
		`, author, content, cw, alt, repostOf).Scan(&id); err != nil {
			t.Fatalf("insert post: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, id) })
		return id
	}

	viewer, friend, bystander := user("viewer"), user("friend"), user("bystander")
	db.Exec(`INSERT INTO friendships (requester_id, addressee_id, status) VALUES ($1, $2, 'accepted')`, viewer, friend)
	db.Exec(`INSERT INTO friendships (requester_id, addressee_id, status) VALUES ($1, $2, 'accepted')`, bystander, friend)

	plain := post(friend, "a lovely day out", "", "", nil)
	spoiler := post(friend, "the finale was great", "", "", nil)
	category := post(friend, "this category is fine", "", "", nil)
	hashtag := post(friend, "look at this #Spoilers thread", "", "", nil)
	inCW := post(friend, "hello", "Politics talk", "", nil)
	inAlt := post(friend, "photo time", "", "A sad clown", nil)
	reposted := post(friend, "original about cricket", "", "", nil)
	repost := post(friend, "", "", "", &reposted)
	special := post(friend, "I write C++ all day", "", "", nil)
	expiring := post(friend, "talking about sourdough", "", "", nil)

	s := &Service{db: db}
	as := func(userID string, req *httptestReq) *httptestRec {
		rctx := chi.NewRouteContext()
		for k, v := range req.params {
			rctx.URLParams.Add(k, v)
		}
		r := req.r.WithContext(context.WithValue(context.WithValue(req.r.Context(), ctxkeys.UserID, userID), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		req.h(w, r)
		return w
	}
	add := func(userID, keyword string, days int) int {
		body, _ := json.Marshal(map[string]any{"keyword": keyword, "days": days})
		r := httptest.NewRequest("POST", "/keyword-mutes", bytes.NewReader(body))
		return as(userID, &httptestReq{r: r, h: s.AddKeywordMute}).Code
	}
	visible := func(userID string) map[string]bool {
		r := httptest.NewRequest("GET", "/feed?limit=100", nil)
		w := as(userID, &httptestReq{r: r, h: s.GetFeed})
		var out struct {
			Posts []Post `json:"posts"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode feed: %v (%s)", err, w.Body.String())
		}
		got := map[string]bool{}
		for _, p := range out.Posts {
			got[p.ID] = true
		}
		return got
	}

	all := []string{plain, spoiler, category, hashtag, inCW, inAlt, reposted, repost, special, expiring}
	if got := visible(viewer); len(got) < len(all) {
		t.Fatalf("baseline: viewer sees %d of %d posts", len(got), len(all))
	}

	for _, k := range []string{"FINALE", "cat", "spoilers", "politics", "clown", "cricket", "c++"} {
		if code := add(viewer, k, 0); code != 200 {
			t.Fatalf("add %q: %d", k, code)
		}
	}
	if code := add(viewer, "sourdough", 1); code != 200 {
		t.Fatalf("add sourdough: %d", code)
	}

	got := visible(viewer)
	for name, id := range map[string]string{"plain": plain, "category (whole word)": category} {
		if !got[id] {
			t.Errorf("%s should still show", name)
		}
	}
	for name, id := range map[string]string{
		"text, case-insensitive": spoiler, "hashtag": hashtag, "content warning": inCW,
		"image description": inAlt, "reposted text": reposted, "repost wrapper": repost,
		"regex metacharacters": special, "timed mute": expiring,
	} {
		if got[id] {
			t.Errorf("%s should be hidden", name)
		}
	}

	// A mute is per user.
	if other := visible(bystander); !other[spoiler] || !other[special] {
		t.Error("mutes leaked to another user")
	}

	// Expiry: lapse the timed mute and it stops applying.
	db.Exec(`UPDATE keyword_mutes SET expires_at = NOW() - INTERVAL '1 minute' WHERE user_id = $1 AND keyword = 'sourdough'`, viewer)
	if !visible(viewer)[expiring] {
		t.Error("expired mute still hides the post")
	}

	// List shows live mutes only; re-adding replaces the duration; remove works.
	list := func() []keywordMute {
		w := as(viewer, &httptestReq{r: httptest.NewRequest("GET", "/keyword-mutes", nil), h: s.ListKeywordMutes})
		var out struct {
			Mutes []keywordMute `json:"mutes"`
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		return out.Mutes
	}
	mutes := list()
	if len(mutes) != 7 {
		t.Fatalf("list: want 7 live mutes, got %d", len(mutes))
	}
	if code := add(viewer, "finale", 7); code != 200 {
		t.Fatalf("re-add: %d", code)
	}
	if len(list()) != 7 {
		t.Error("re-adding a keyword with different case created a duplicate")
	}
	if add(viewer, "   ", 0) != 400 || add(viewer, "x", -1) != 400 {
		t.Error("blank keyword or negative days should be rejected")
	}
	for _, m := range mutes {
		if m.Keyword == "cat" {
			as(viewer, &httptestReq{r: httptest.NewRequest("DELETE", "/keyword-mutes/"+m.ID, nil), h: s.RemoveKeywordMute, params: map[string]string{"id": m.ID}})
		}
	}
	// Nobody else can remove it: the bystander has no 'cat' mute row to affect, and
	// removing viewer's id as bystander leaves viewer's rows alone.
	if len(list()) != 6 {
		t.Errorf("remove: want 6 mutes, got %d", len(list()))
	}
	as(bystander, &httptestReq{r: httptest.NewRequest("DELETE", "/keyword-mutes/"+mutes[0].ID, nil), h: s.RemoveKeywordMute, params: map[string]string{"id": mutes[0].ID}})
	if len(list()) != 6 {
		t.Error("another user removed this user's mute")
	}
	if !visible(viewer)[category] {
		t.Error("category still shows after removing cat")
	}
}
