package notifications

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-420: a type with no push wording is created, listed in the app, and
// never pushed. These are every type the backend creates; a new one has to be
// added here and given wording, or deliberately left out with a reason.
func TestPushNotifContentCoversCreatedTypes(t *testing.T) {
	types := []string{
		"friend_request", "friend_accepted",
		"post_like", "comment_like", "post_reaction", "comment_reaction",
		"post_comment", "comment_reply", "post_mention", "post_repost",
		"wall_post", "wall_post_pending", "wall_post_approved",
		"user_post", "fediverse_post", "atproto_post", "page_post",
		"fediverse_follow", "atproto_follow",
		"group_join_request", "group_join_approved", "group_join_rejected", "group_invite_accepted", "group_tag",
		"page_member_invite",
		"new_report", "waitlist_join", "federation_request",
		"custom_domain_live", "custom_domain_verified", "custom_domain_failed", "custom_domain_lost", "custom_domain_rejected",
	}
	for _, typ := range types {
		title, body := pushNotifContent(typ, "Alex", "example.com")
		if title == "" || body == "" {
			t.Errorf("pushNotifContent(%q) has no wording, so it would never be pushed", typ)
		}
	}
}

func pushTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	// Registered first so it runs last, after the row cleanups below.
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func pushTestUser(t *testing.T, db *store.DB, tag string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`,
		fmt.Sprintf("p420_%s_%d", tag, time.Now().UnixNano())).Scan(&id); err != nil {
		t.Fatalf("seeding %s: %v", tag, err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
	return id
}

// A DeviceNotRegistered ticket drops that device's token and leaves the
// user's other devices alone; any other error keeps the token.
func TestHandleTicketsDropsOnlyUnregisteredTokens(t *testing.T) {
	db := pushTestDB(t)
	s := &Service{db: db}
	user := pushTestUser(t, db, "u")
	suffix := time.Now().UnixNano()
	dead := fmt.Sprintf("ExponentPushToken[dead-%d]", suffix)
	live := fmt.Sprintf("ExponentPushToken[live-%d]", suffix)
	flaky := fmt.Sprintf("ExponentPushToken[flaky-%d]", suffix)
	for _, tok := range []string{dead, live, flaky} {
		if _, err := db.Exec(`INSERT INTO push_tokens (token, user_id) VALUES ($1, $2)`, tok, user); err != nil {
			t.Fatalf("seeding token: %v", err)
		}
	}
	db.Exec(`UPDATE users SET expo_push_token = $1 WHERE id = $2`, dead, user)

	messages := []expoMessage{{To: dead}, {To: live}, {To: flaky}}
	tickets := make([]expoTicket, 3)
	tickets[0].Status, tickets[0].Details.Error = "error", "DeviceNotRegistered"
	tickets[1].Status = "ok"
	tickets[2].Status, tickets[2].Details.Error = "error", "MessageRateExceeded"
	s.handleTickets(messages, tickets)

	got := map[string]bool{}
	for _, tok := range s.pushTokens(user) {
		got[tok] = true
	}
	if got[dead] || !got[live] || !got[flaky] {
		t.Errorf("tokens after handleTickets = %v; want only %q and %q", got, live, flaky)
	}
	var column string
	db.QueryRow(`SELECT expo_push_token FROM users WHERE id = $1`, user).Scan(&column)
	if column != "" {
		t.Errorf("users.expo_push_token = %q, want it cleared so the startup backfill can't bring it back", column)
	}
}

// AGORA-425: the icon's badge counts unread messages as well as unread
// notifications, but not the user's own messages or ones already read.
func TestBadgeCountIncludesUnreadMessages(t *testing.T) {
	db := pushTestDB(t)
	s := &Service{db: db}
	me, them := pushTestUser(t, db, "me"), pushTestUser(t, db, "them")
	var convID string
	db.QueryRow(`INSERT INTO conversations DEFAULT VALUES RETURNING id`).Scan(&convID)
	t.Cleanup(func() { db.Exec(`DELETE FROM conversations WHERE id = $1`, convID) })
	db.Exec(`INSERT INTO conversation_participants (conversation_id, user_id, is_accepted, last_read_at) VALUES ($1,$2,true,NOW() - interval '1 hour')`, convID, me)
	db.Exec(`INSERT INTO conversation_participants (conversation_id, user_id, is_accepted) VALUES ($1,$2,true)`, convID, them)
	db.Exec(`INSERT INTO messages (conversation_id, author_id, content, created_at) VALUES ($1,$2,'old',NOW() - interval '2 hours')`, convID, them)
	db.Exec(`INSERT INTO messages (conversation_id, author_id, content) VALUES ($1,$2,'new one')`, convID, them)
	db.Exec(`INSERT INTO messages (conversation_id, author_id, content) VALUES ($1,$2,'new two')`, convID, them)
	db.Exec(`INSERT INTO messages (conversation_id, author_id, content) VALUES ($1,$2,'mine')`, convID, me)
	db.Exec(`INSERT INTO notifications (user_id, type) VALUES ($1, 'friend_request')`, me)

	if got := s.badgeCount(me); got != 3 {
		t.Errorf("badgeCount = %d, want 3 (two unread messages and one notification)", got)
	}
}

// A comment's notification opens the post it's on.
func TestRootPostIDResolvesComments(t *testing.T) {
	db := pushTestDB(t)
	s := &Service{db: db}
	author := pushTestUser(t, db, "a")
	var root, comment, reply string
	db.QueryRow(`INSERT INTO posts (author_id, content) VALUES ($1, 'root') RETURNING id`, author).Scan(&root)
	db.QueryRow(`INSERT INTO posts (author_id, content, parent_id) VALUES ($1, 'c', $2) RETURNING id`, author, root).Scan(&comment)
	db.QueryRow(`INSERT INTO posts (author_id, content, parent_id) VALUES ($1, 'r', $2) RETURNING id`, author, comment).Scan(&reply)
	for _, id := range []string{root, comment, reply} {
		if got := s.rootPostID(id); got != root {
			t.Errorf("rootPostID(%s) = %s, want %s", id, got, root)
		}
	}
}
