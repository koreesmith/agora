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

// AGORA-426: the person who blocked an account still gets its profile, marked
// is_blocked so they can unblock it; the blocked account gets user not found.
func TestGetProfileAcrossABlock(t *testing.T) {
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
		name := fmt.Sprintf("bp426_%s_%d", tag, unique)
		if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`, name).Scan(&id); err != nil {
			t.Fatalf("seeding %s: %v", tag, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id, name
	}
	blocker, blockerName := mkUser("blocker")
	blocked, blockedName := mkUser("blocked")
	db.Exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1,$2)`, blocker, blocked)

	s := &Service{db: db}
	get := func(viewer, username string) (int, map[string]any) {
		rc := chi.NewRouteContext()
		rc.URLParams.Add("username", username)
		ctx := context.WithValue(context.Background(), ctxkeys.UserID, viewer)
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rc)
		w := httptest.NewRecorder()
		s.GetProfile(w, httptest.NewRequest("GET", "/users/"+username, nil).WithContext(ctx))
		var body map[string]any
		json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code, body
	}

	if code, body := get(blocker, blockedName); code != 200 || body["is_blocked"] != true {
		t.Errorf("blocker viewing the blocked account: %d %v, want 200 with is_blocked true", code, body["is_blocked"])
	}
	if code, _ := get(blocked, blockerName); code != 404 {
		t.Errorf("blocked account viewing its blocker: %d, want 404", code)
	}
}
