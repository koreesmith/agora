package users

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/ctxkeys"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-387 tests. Like the rest of this package's DB-backed tests they need
// the local agora-postgres-test instance (localhost:15433) and skip if it
// isn't reachable rather than failing the suite.

func openOnboardingTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func insertOnboardingUser(t *testing.T, db *store.DB, name string, private bool) string {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var id string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, email_verified, profile_private)
		VALUES ($1, $2, 'x', true, $3)
		RETURNING id
	`, name+suffix, name+suffix+"@example.com", private).Scan(&id); err != nil {
		t.Fatalf("insert user %s: %v", name, err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
	return id
}

// The backfill stamps the accounts that existed when it first ran. A rerun,
// which happens on every restart, must leave accounts created since then
// pending, or nobody would ever see the wizard.
func TestOnboardingBackfillLeavesNewAccountsPending(t *testing.T) {
	db := openOnboardingTestDB(t)

	id := insertOnboardingUser(t, db, "agora387new", false)
	if err := db.Migrate(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	var completed sql.NullTime
	if err := db.QueryRow(`SELECT onboarding_completed_at FROM users WHERE id = $1`, id).Scan(&completed); err != nil {
		t.Fatalf("read onboarding_completed_at: %v", err)
	}
	if completed.Valid {
		t.Fatalf("a rerun of the backfill stamped an account created after it: onboarding_completed_at = %v", completed.Time)
	}
}

func TestCompleteOnboardingStampsOnceAndKeepsTimestamp(t *testing.T) {
	db := openOnboardingTestDB(t)
	s := &Service{db: db}
	id := insertOnboardingUser(t, db, "agora387done", false)

	complete := func() time.Time {
		t.Helper()
		ctx := context.WithValue(context.Background(), ctxkeys.UserID, id)
		req := httptest.NewRequest("POST", "/onboarding/complete", nil).WithContext(ctx)
		w := httptest.NewRecorder()
		s.CompleteOnboarding(w, req)
		if w.Code != 200 {
			t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		var at sql.NullTime
		db.QueryRow(`SELECT onboarding_completed_at FROM users WHERE id = $1`, id).Scan(&at)
		if !at.Valid {
			t.Fatal("onboarding_completed_at still NULL after complete")
		}
		return at.Time
	}

	first := complete()
	second := complete()
	if !first.Equal(second) {
		t.Fatalf("a second complete moved the timestamp: %v then %v", first, second)
	}
}

func TestOnboardingSuggestions(t *testing.T) {
	db := openOnboardingTestDB(t)
	s := &Service{db: db}

	caller := insertOnboardingUser(t, db, "agora387caller", false)
	inviter := insertOnboardingUser(t, db, "agora387inviter", true)
	friend := insertOnboardingUser(t, db, "agora387friend", false)
	private := insertOnboardingUser(t, db, "agora387private", true)
	requested := insertOnboardingUser(t, db, "agora387requested", false)
	recent := insertOnboardingUser(t, db, "agora387recent", false)

	code := fmt.Sprintf("agora387-%d", time.Now().UnixNano())
	if _, err := db.Exec(`
		INSERT INTO invite_codes (code, created_by, used_by, used_at) VALUES ($1, $2, $3, NOW())
	`, code, inviter, caller); err != nil {
		t.Fatalf("insert invite code: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM invite_codes WHERE code = $1`, code) })

	if _, err := db.Exec(`
		INSERT INTO friendships (requester_id, addressee_id, status) VALUES ($1, $2, 'accepted'), ($1, $3, 'pending')
	`, caller, friend, requested); err != nil {
		t.Fatalf("insert friendships: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM friendships WHERE requester_id = $1`, caller)
	})

	ctx := context.WithValue(context.Background(), ctxkeys.UserID, caller)
	req := httptest.NewRequest("GET", "/onboarding/suggestions", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	s.OnboardingSuggestions(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body struct {
		Inviter *onboardingPerson  `json:"inviter"`
		People  []onboardingPerson `json:"people"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// A private inviter is still shown: they reached out first.
	if body.Inviter == nil || body.Inviter.ID != inviter {
		t.Fatalf("inviter = %+v, want %s", body.Inviter, inviter)
	}

	statusByID := map[string]string{}
	for _, p := range body.People {
		statusByID[p.ID] = p.FriendStatus
	}
	for name, id := range map[string]string{"caller": caller, "inviter": inviter, "friend": friend, "private": private} {
		if _, ok := statusByID[id]; ok {
			t.Errorf("people includes the %s (%s), which it must leave out", name, id)
		}
	}
	if got, ok := statusByID[recent]; !ok || got != "" {
		t.Errorf("recent member: present = %v, friend_status = %q; want present with no status", ok, got)
	}
	if got, ok := statusByID[requested]; !ok || got != "pending" {
		t.Errorf("already-requested member: present = %v, friend_status = %q; want present with \"pending\"", ok, got)
	}
}

// With no invite code behind the account there is no inviter, and that must
// not exclude anybody from the people list.
func TestOnboardingSuggestionsWithoutInviter(t *testing.T) {
	db := openOnboardingTestDB(t)
	s := &Service{db: db}

	caller := insertOnboardingUser(t, db, "agora387solo", false)
	recent := insertOnboardingUser(t, db, "agora387solorecent", false)

	ctx := context.WithValue(context.Background(), ctxkeys.UserID, caller)
	req := httptest.NewRequest("GET", "/onboarding/suggestions", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	s.OnboardingSuggestions(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body struct {
		Inviter *onboardingPerson  `json:"inviter"`
		People  []onboardingPerson `json:"people"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Inviter != nil {
		t.Fatalf("inviter = %+v, want null", body.Inviter)
	}
	found := false
	for _, p := range body.People {
		if p.ID == recent {
			found = true
		}
	}
	if !found {
		t.Fatalf("people = %+v, want it to include the newest member %s", body.People, recent)
	}
}
