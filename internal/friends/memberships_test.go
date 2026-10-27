package friends

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/ctxkeys"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-395: the add-to-lists modal used to load an account's membership with
// one request per list and save with one request per changed list, so a burst
// that got some of them refused left it guessing at membership and applying
// whichever writes got through. The memberships endpoints answer and change
// one account's lists in a single request each, and the change is all or
// nothing.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestMemberships(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil { t.Skipf("skipping: agora-postgres-test not reachable: %v", err) }
	defer db.Close()
	if err := db.Migrate(); err != nil { t.Fatalf("migrate: %v", err) }
	u := time.Now().UnixNano()

	mkUser := func(tag string) string {
		var id string
		if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`,
			fmt.Sprintf("ms_%s_%d", tag, u)).Scan(&id); err != nil {
			t.Fatalf("seed user: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id=$1`, id) })
		return id
	}
	owner    := mkUser("own")
	other    := mkUser("oth")
	friend   := mkUser("fr")
	stranger := mkUser("str")
	db.Exec(`INSERT INTO friendships (requester_id,addressee_id,status) VALUES ($1,$2,'accepted')`, owner, friend)

	// A followed Mastodon account and a followed Bluesky account, the two
	// non-friend kinds of connection a list can hold.
	var masto, bsky string
	db.QueryRow(`INSERT INTO users (username,email,password_hash,is_remote,remote_instance,ap_actor_url)
		VALUES ($1,$1,'x',true,'mastodon.social',$2) RETURNING id`,
		fmt.Sprintf("ms_md_%d", u), fmt.Sprintf("https://mastodon.social/users/ms%d", u)).Scan(&masto)
	db.QueryRow(`INSERT INTO users (username,email,password_hash,is_remote,remote_instance,atproto_remote_did)
		VALUES ($1,$1,'x',true,'bsky.app',$2) RETURNING id`,
		fmt.Sprintf("ms_bs_%d", u), fmt.Sprintf("did:plc:ms%d", u)).Scan(&bsky)
	for _, id := range []string{masto, bsky} {
		id := id
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	db.Exec(`INSERT INTO ap_following (follower_user_id,followed_actor_url,followed_inbox_url,accepted) VALUES ($1,$2,'',true)`,
		owner, fmt.Sprintf("https://mastodon.social/users/ms%d", u))
	db.Exec(`INSERT INTO at_following (local_user_id,remote_did) VALUES ($1,$2)`, owner, fmt.Sprintf("did:plc:ms%d", u))

	mkList := func(userID, name string) string {
		var id string
		if err := db.QueryRow(`INSERT INTO friend_groups (user_id,name) VALUES ($1,$2) RETURNING id`, userID, name).Scan(&id); err != nil {
			t.Fatalf("seed list: %v", err)
		}
		return id
	}
	a, b, c := mkList(owner, "A"), mkList(owner, "B"), mkList(owner, "C")
	othersList := mkList(other, "Theirs")

	s := NewService(db, nil)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), ctxkeys.UserID, req.Header.Get("X-Test-User"))))
		})
	})
	RegisterRoutes(r, s)

	call := func(method, path, body string) (int, map[string]any) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-Test-User", owner)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	groupIDs := func(out map[string]any) []string {
		ids := []string{}
		for _, v := range out["group_ids"].([]any) { ids = append(ids, v.(string)) }
		sort.Strings(ids)
		return ids
	}
	stored := func(friendID string) []string {
		rows, _ := db.Query(`SELECT group_id FROM friend_group_members WHERE friend_id=$1`, friendID)
		defer rows.Close()
		ids := []string{}
		for rows.Next() { var id string; rows.Scan(&id); ids = append(ids, id) }
		sort.Strings(ids)
		return ids
	}
	sorted := func(ids ...string) []string { sort.Strings(ids); return ids }
	put := func(friendID string, add, remove []string) (int, map[string]any) {
		body, _ := json.Marshal(map[string]any{"add": add, "remove": remove})
		return call("PUT", "/friend-groups/memberships/"+friendID, string(body))
	}

	// The account is on A and B, and on somebody else's list too, which is
	// none of the caller's business.
	db.Exec(`INSERT INTO friend_group_members (group_id,friend_id) VALUES ($1,$4),($2,$4),($3,$4)`, a, b, othersList, friend)

	t.Run("lists only the caller's lists the account is on", func(t *testing.T) {
		code, out := call("GET", "/friend-groups/memberships/"+friend, "")
		if code != 200 { t.Fatalf("status = %d, want 200", code) }
		if got, want := groupIDs(out), sorted(a, b); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("group_ids = %v, want %v", got, want)
		}
	})

	t.Run("adds and removes in one request", func(t *testing.T) {
		code, out := put(friend, []string{c}, []string{a})
		if code != 200 { t.Fatalf("status = %d, want 200 (%v)", code, out) }
		want := sorted(b, c)
		if got := groupIDs(out); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("response group_ids = %v, want %v", got, want)
		}
		if got := stored(friend); fmt.Sprint(got) != fmt.Sprint(sorted(b, c, othersList)) {
			t.Errorf("stored = %v, want B, C and the other user's list untouched", got)
		}
	})

	t.Run("is idempotent, so a retry is safe", func(t *testing.T) {
		code, out := put(friend, []string{c}, []string{a})
		if code != 200 { t.Fatalf("status = %d, want 200 (%v)", code, out) }
		if got := groupIDs(out); fmt.Sprint(got) != fmt.Sprint(sorted(b, c)) {
			t.Errorf("group_ids = %v, want %v", got, sorted(b, c))
		}
	})

	t.Run("applies nothing if any list is not the caller's", func(t *testing.T) {
		before := stored(friend)
		code, _ := put(friend, []string{a, othersList}, []string{b})
		if code != 409 { t.Errorf("status = %d, want 409", code) }
		if got := stored(friend); fmt.Sprint(got) != fmt.Sprint(before) {
			t.Errorf("stored = %v, want unchanged %v: the valid add and remove must not apply on their own", got, before)
		}
	})

	t.Run("applies nothing if a list was deleted meanwhile", func(t *testing.T) {
		gone := mkList(owner, "Gone")
		db.Exec(`DELETE FROM friend_groups WHERE id=$1`, gone)
		before := stored(friend)
		code, _ := put(friend, []string{a, gone}, nil)
		if code != 409 { t.Errorf("status = %d, want 409", code) }
		if got := stored(friend); fmt.Sprint(got) != fmt.Sprint(before) {
			t.Errorf("stored = %v, want unchanged %v", got, before)
		}
	})

	t.Run("refuses to add an account the caller is not connected to", func(t *testing.T) {
		code, out := put(stranger, []string{a}, nil)
		if code != 400 { t.Errorf("status = %d, want 400 (%v)", code, out) }
		if got := stored(stranger); len(got) != 0 {
			t.Errorf("stored = %v, want nothing", got)
		}
	})

	t.Run("still lets a no-longer-connected account be removed", func(t *testing.T) {
		db.Exec(`INSERT INTO friend_group_members (group_id,friend_id) VALUES ($1,$2)`, a, stranger)
		code, out := put(stranger, nil, []string{a})
		if code != 200 { t.Fatalf("status = %d, want 200 (%v)", code, out) }
		if got := stored(stranger); len(got) != 0 {
			t.Errorf("stored = %v, want nothing", got)
		}
	})

	t.Run("followed Fediverse and Bluesky accounts can be added", func(t *testing.T) {
		for name, id := range map[string]string{"fediverse": masto, "bluesky": bsky} {
			code, out := put(id, []string{a, c}, nil)
			if code != 200 { t.Errorf("%s: status = %d, want 200 (%v)", name, code, out); continue }
			if got := groupIDs(out); fmt.Sprint(got) != fmt.Sprint(sorted(a, c)) {
				t.Errorf("%s: group_ids = %v, want %v", name, got, sorted(a, c))
			}
		}
	})

	t.Run("rejects malformed ids and contradictions", func(t *testing.T) {
		for name, tc := range map[string]struct{ friendID, body string }{
			"account id":         {"not-a-uuid", `{"add":[]}`},
			"list id":            {friend, `{"add":["nope"]}`},
			"add and remove one": {friend, fmt.Sprintf(`{"add":[%q],"remove":[%q]}`, a, a)},
			"body":               {friend, `{`},
		} {
			if code, _ := call("PUT", "/friend-groups/memberships/"+tc.friendID, tc.body); code != 400 {
				t.Errorf("%s: status = %d, want 400", name, code)
			}
		}
		if code, _ := call("GET", "/friend-groups/memberships/not-a-uuid", ""); code != 400 {
			t.Errorf("GET with a malformed account id: status = %d, want 400", code)
		}
	})

	t.Run("per-list endpoints treat a malformed list id as not the caller's", func(t *testing.T) {
		for _, tc := range []struct{ method, path string }{
			{"GET", "/friend-groups/nope/members"},
			{"POST", "/friend-groups/nope/members/" + friend},
			{"DELETE", "/friend-groups/nope/members/" + friend},
		} {
			if code, _ := call(tc.method, tc.path, ""); code != 403 {
				t.Errorf("%s %s: status = %d, want 403", tc.method, tc.path, code)
			}
		}
	})
}
