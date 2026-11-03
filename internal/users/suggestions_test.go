package users

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/ctxkeys"
)

// AGORA-423 tests. Same arrangement as the onboarding tests: they need the
// local agora-postgres-test instance and skip if it is not reachable.

func befriend(t *testing.T, s *Service, a, b string) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO friendships (requester_id, addressee_id, status) VALUES ($1, $2, 'accepted')`, a, b); err != nil {
		t.Fatalf("befriend: %v", err)
	}
	t.Cleanup(func() { s.db.Exec(`DELETE FROM friendships WHERE requester_id = $1 AND addressee_id = $2`, a, b) })
}

func suggestionIndex(list []DiscoverUser, id string) int {
	for i, u := range list {
		if u.ID == id {
			return i
		}
	}
	return -1
}

func TestRankedLocalSuggestions(t *testing.T) {
	db := openOnboardingTestDB(t)
	s := &Service{db: db}

	me := insertOnboardingUser(t, db, "agora423me", false)
	bridge := insertOnboardingUser(t, db, "agora423bridge", false)
	mutual := insertOnboardingUser(t, db, "agora423mutual", false)
	stranger := insertOnboardingUser(t, db, "agora423stranger", false)
	privateMutual := insertOnboardingUser(t, db, "agora423privatemutual", true)
	privateStranger := insertOnboardingUser(t, db, "agora423privatestranger", true)
	blocked := insertOnboardingUser(t, db, "agora423blocked", false)
	declined := insertOnboardingUser(t, db, "agora423declined", false)
	dismissed := insertOnboardingUser(t, db, "agora423dismissed", false)
	suspended := insertOnboardingUser(t, db, "agora423suspended", false)
	friend := insertOnboardingUser(t, db, "agora423friend", false)

	befriend(t, s, me, bridge)
	befriend(t, s, bridge, mutual)
	befriend(t, s, bridge, privateMutual)
	befriend(t, s, bridge, blocked)
	befriend(t, s, bridge, dismissed)
	befriend(t, s, bridge, suspended)
	befriend(t, s, me, friend)

	db.Exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, me, blocked)
	db.Exec(`INSERT INTO friendships (requester_id, addressee_id, status) VALUES ($1, $2, 'declined')`, me, declined)
	db.Exec(`INSERT INTO suggestion_dismissals (user_id, kind, target) VALUES ($1, 'user', $2)`, me, dismissed)
	db.Exec(`UPDATE users SET is_suspended = true WHERE id = $1`, suspended)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM blocks WHERE blocker_id = $1`, me)
		db.Exec(`DELETE FROM friendships WHERE requester_id = $1 AND addressee_id = $2`, me, declined)
		db.Exec(`DELETE FROM suggestion_dismissals WHERE user_id = $1`, me)
	})

	got, err := s.rankedLocalSuggestions(me, "", 500)
	if err != nil {
		t.Fatalf("rankedLocalSuggestions: %v", err)
	}

	mi, si := suggestionIndex(got, mutual), suggestionIndex(got, stranger)
	if mi < 0 || si < 0 {
		t.Fatalf("mutual present = %v, stranger present = %v; want both", mi >= 0, si >= 0)
	}
	if mi > si {
		t.Errorf("friend of a friend ranked at %d, below a stranger at %d", mi, si)
	}
	if got[mi].MutualCount != 1 || len(got[mi].Reasons) == 0 {
		t.Errorf("mutual: count = %d, reasons = %v; want 1 mutual friend with a reason", got[mi].MutualCount, got[mi].Reasons)
	}
	if suggestionIndex(got, privateMutual) < 0 {
		t.Errorf("a private profile with a mutual friend was not suggested")
	}
	for name, id := range map[string]string{
		"private stranger": privateStranger, "blocked": blocked, "declined": declined,
		"dismissed": dismissed, "suspended": suspended, "friend": friend, "self": me,
	} {
		if suggestionIndex(got, id) >= 0 {
			t.Errorf("%s was suggested", name)
		}
	}
}

func TestSharedGroupAndInteractionRaiseRank(t *testing.T) {
	db := openOnboardingTestDB(t)
	s := &Service{db: db}

	me := insertOnboardingUser(t, db, "agora423signalme", false)
	grouped := insertOnboardingUser(t, db, "agora423grouped", false)
	replied := insertOnboardingUser(t, db, "agora423replied", false)
	plain := insertOnboardingUser(t, db, "agora423plain", false)

	var groupID string
	if err := db.QueryRow(`
		INSERT INTO community_groups (name, slug, created_by) VALUES ($1, $2, $3) RETURNING id
	`, "Hikers", fmt.Sprintf("agora423-%d", time.Now().UnixNano()), me).Scan(&groupID); err != nil {
		t.Fatalf("insert group: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM community_groups WHERE id = $1`, groupID) })
	db.Exec(`INSERT INTO community_group_members (group_id, user_id) VALUES ($1, $2), ($1, $3)`, groupID, me, grouped)

	var postID string
	db.QueryRow(`INSERT INTO posts (author_id, content, visibility) VALUES ($1, 'hi', 'public') RETURNING id`, me).Scan(&postID)
	db.Exec(`INSERT INTO posts (author_id, content, visibility, parent_id) VALUES ($1, 'reply', 'public', $2)`, replied, postID)
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE author_id IN ($1, $2)`, me, replied) })

	got, err := s.rankedLocalSuggestions(me, "", 500)
	if err != nil {
		t.Fatalf("rankedLocalSuggestions: %v", err)
	}
	gi, ri, pi := suggestionIndex(got, grouped), suggestionIndex(got, replied), suggestionIndex(got, plain)
	if gi < 0 || ri < 0 || pi < 0 {
		t.Fatalf("grouped = %d, replied = %d, plain = %d; want all present", gi, ri, pi)
	}
	if gi > pi || ri > pi {
		t.Errorf("signals did not outrank plain: grouped %d, replied %d, plain %d", gi, ri, pi)
	}
	if len(got[gi].Reasons) == 0 || got[gi].Reasons[0] != "In Hikers with you" {
		t.Errorf("grouped reasons = %v, want first to be \"In Hikers with you\"", got[gi].Reasons)
	}
}

func TestDismissSuggestionAndFediverseSection(t *testing.T) {
	db := openOnboardingTestDB(t)
	s := &Service{db: db}

	me := insertOnboardingUser(t, db, "agora423fedme", false)
	actor := fmt.Sprintf("https://remote%d.example/users/amy", time.Now().UnixNano())
	var stub string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, email_verified, is_remote, remote_instance, ap_actor_url)
		VALUES ($1, $2, 'x', true, true, 'remote.example', $3) RETURNING id
	`, fmt.Sprintf("amy%d", time.Now().UnixNano()), fmt.Sprintf("amy%d@remote.invalid", time.Now().UnixNano()), actor).Scan(&stub); err != nil {
		t.Fatalf("insert stub: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, stub) })
	db.Exec(`INSERT INTO ap_followers (followed_user_id, follower_actor_url, follower_inbox_url) VALUES ($1, $2, $2)`, me, actor)
	t.Cleanup(func() { db.Exec(`DELETE FROM suggestion_dismissals WHERE user_id = $1`, me) })

	has := func() bool {
		for _, f := range s.fediverseSuggestions(me, 50) {
			if f.ActorURL == actor {
				return f.Reason == "Follows you"
			}
		}
		return false
	}
	if !has() {
		t.Fatalf("a Fediverse account that follows the viewer was not suggested")
	}

	body, _ := json.Marshal(map[string]string{"kind": "fediverse", "key": actor})
	ctx := context.WithValue(context.Background(), ctxkeys.UserID, me)
	w := httptest.NewRecorder()
	s.DismissSuggestion(w, httptest.NewRequest("POST", "/users/discover/dismiss", bytes.NewReader(body)).WithContext(ctx))
	if w.Code != 200 {
		t.Fatalf("dismiss status = %d; body: %s", w.Code, w.Body.String())
	}
	if has() {
		t.Errorf("a dismissed Fediverse suggestion still appeared")
	}

	bad, _ := json.Marshal(map[string]string{"kind": "nope", "key": "x"})
	w = httptest.NewRecorder()
	s.DismissSuggestion(w, httptest.NewRequest("POST", "/users/discover/dismiss", bytes.NewReader(bad)).WithContext(ctx))
	if w.Code != 400 {
		t.Errorf("unknown kind status = %d, want 400", w.Code)
	}
}
