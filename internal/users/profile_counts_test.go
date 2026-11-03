package users

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

// AGORA-422: a local profile's follower and following totals span every
// network the account is on. A friend is mutual, so counts on both sides; a
// fediverse follow still awaiting an Accept is not a follow yet.
func TestGetProfileFollowerFollowingTotals(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	unique := time.Now().UnixNano()
	mkUser := func(tag string) (string, string) {
		var id string
		name := fmt.Sprintf("pc422_%s_%d", tag, unique)
		if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`, name).Scan(&id); err != nil {
			t.Fatalf("seeding %s: %v", tag, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id, name
	}
	me, myName := mkUser("me")
	friend, _ := mkUser("friend")

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO friendships (requester_id, addressee_id, status) VALUES ($1,$2,'accepted')`, me, friend)
	exec(`INSERT INTO ap_followers (followed_user_id, follower_actor_url, follower_inbox_url) VALUES ($1,$2,$3)`,
		me, fmt.Sprintf("https://m.example/users/f_%d", unique), "https://m.example/inbox")
	exec(`INSERT INTO at_followers (local_user_id, follower_did) VALUES ($1,$2), ($1,$3)`,
		me, fmt.Sprintf("did:plc:a%d", unique), fmt.Sprintf("did:plc:b%d", unique))
	exec(`INSERT INTO ap_following (follower_user_id, followed_actor_url, followed_inbox_url, accepted) VALUES ($1,$2,$3,true), ($1,$4,$5,false)`,
		me, fmt.Sprintf("https://m.example/users/a_%d", unique), "https://m.example/a/inbox",
		fmt.Sprintf("https://m.example/users/p_%d", unique), "https://m.example/p/inbox")
	exec(`INSERT INTO at_following (local_user_id, remote_did) VALUES ($1,$2)`, me, fmt.Sprintf("did:plc:c%d", unique))

	s := &Service{db: db}
	rc := chi.NewRouteContext()
	rc.URLParams.Add("username", myName)
	ctx := context.WithValue(context.Background(), ctxkeys.UserID, me)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rc)
	w := httptest.NewRecorder()
	s.GetProfile(w, httptest.NewRequest("GET", "/users/"+myName, nil).WithContext(ctx))
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)

	// 1 friend + 1 fediverse follower + 2 Bluesky followers
	if got := body["follower_count"]; got != float64(4) {
		t.Errorf("follower_count = %v, want 4", got)
	}
	// 1 friend + 1 accepted fediverse follow (the pending one is excluded) + 1 Bluesky follow
	if got := body["following_count"]; got != float64(3) {
		t.Errorf("following_count = %v, want 3", got)
	}
}
