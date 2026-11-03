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

// AGORA-432: a search result for a remote account the viewer already follows
// must say so. Needs the local agora-postgres-test instance; skips otherwise.
func TestSearchUsersReportsFollowState(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	n := time.Now().UnixNano()
	insert := func(name string, remote bool, instance, actorURL, did string) string {
		var id string
		if err := db.QueryRow(`
			INSERT INTO users (username, email, password_hash, email_verified, is_remote, remote_instance, ap_actor_url, atproto_remote_did)
			VALUES ($1, $2, 'x', true, $3, $4, $5, $6) RETURNING id
		`, fmt.Sprintf("%s%d", name, n), fmt.Sprintf("%s%d@x.invalid", name, n), remote, instance, actorURL, did).Scan(&id); err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}

	viewer := insert("agora432viewer", false, "", "", "")
	actor := fmt.Sprintf("https://m.example/users/agora432fedi%d", n)
	pendingActor := fmt.Sprintf("https://m.example/users/agora432pend%d", n)
	did := fmt.Sprintf("did:plc:agora432%d", n)
	insert("agora432fedi", true, "m.example", actor, "")
	insert("agora432pend", true, "m.example", pendingActor, "")
	insert("agora432sky", true, "bsky.app", "", did)
	insert("agora432stranger", true, "m.example", fmt.Sprintf("https://m.example/users/agora432str%d", n), "")

	db.Exec(`INSERT INTO ap_following (follower_user_id, followed_actor_url, followed_inbox_url, accepted) VALUES ($1, $2, $2, true)`, viewer, actor)
	db.Exec(`INSERT INTO ap_following (follower_user_id, followed_actor_url, followed_inbox_url, accepted) VALUES ($1, $2, $2, false)`, viewer, pendingActor)
	db.Exec(`INSERT INTO at_following (local_user_id, remote_did, remote_handle) VALUES ($1, $2, 'sky.test')`, viewer, did)

	req := httptest.NewRequest("GET", "/search/users?q=agora432", nil).
		WithContext(context.WithValue(context.Background(), ctxkeys.UserID, viewer))
	w := httptest.NewRecorder()
	NewService(db).SearchUsers(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	var body struct {
		Users []struct {
			Username      string `json:"username"`
			Following     bool   `json:"following"`
			FollowPending bool   `json:"follow_pending"`
		} `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	type state struct{ following, pending bool }
	want := map[string]state{
		fmt.Sprintf("agora432fedi%d", n):     {true, false},
		fmt.Sprintf("agora432pend%d", n):     {false, true},
		fmt.Sprintf("agora432sky%d", n):      {true, false},
		fmt.Sprintf("agora432stranger%d", n): {false, false},
	}
	seen := 0
	for _, u := range body.Users {
		exp, ok := want[u.Username]
		if !ok {
			continue
		}
		seen++
		if u.Following != exp.following || u.FollowPending != exp.pending {
			t.Errorf("%s: following = %v, follow_pending = %v; want %v, %v", u.Username, u.Following, u.FollowPending, exp.following, exp.pending)
		}
	}
	if seen != len(want) {
		t.Errorf("found %d of %d seeded accounts in the results", seen, len(want))
	}
}
