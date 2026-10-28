package feed

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-401: only a bare @name is a local mention. The name half of a
// Fediverse @name@instance and a dotted @name.bsky.social are other people,
// so a local account that shares the name must not get their badge.
func TestMentionedLocalUsernames(t *testing.T) {
	got := mentionedLocalUsernames("hi @Alice and @bob@mastodon.social, @carol.bsky.social, and @dave.")
	want := []string{"alice", "dave"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := mentionedLocalUsernames("no mentions here"); got != nil {
		t.Errorf("no mentions: got %q", got)
	}
}

// AGORA-401: a post's mention_domains holds the live custom domain of each
// mentioned local account that has one, and nothing for anyone else.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestMentionDomains(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil { t.Skipf("skipping: agora-postgres-test not reachable: %v", err) }
	t.Cleanup(func() { db.Close() }) // registered first, so it runs last
	if err := db.Migrate(); err != nil { t.Fatalf("migrate: %v", err) }

	u := time.Now().UnixNano()
	mkUser := func(tag string) (string, string) {
		name := fmt.Sprintf("md401_%s_%d", tag, u)
		var id string
		if err := db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1, $1, 'x') RETURNING id`, name).Scan(&id); err != nil {
			t.Fatalf("insert user: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id, name
	}
	liveID, live := mkUser("live")
	pendingID, pending := mkUser("pend")
	_, plain := mkUser("plain")
	domain := fmt.Sprintf("live%d.example", u)
	db.Exec(`INSERT INTO custom_domains (user_id, domain, verification_status, approval_status) VALUES ($1, $2, 'verified', 'approved')`, liveID, domain)
	db.Exec(`INSERT INTO custom_domains (user_id, domain, verification_status, approval_status) VALUES ($1, $2, 'verified', 'pending')`, pendingID, "pending"+domain)

	s := &Service{db: db}
	posts := []Post{
		{ID: "p1", AuthorUsername: plain, Content: fmt.Sprintf("thanks @%s, @%s and @%s", live, pending, plain)},
		{ID: "p2", AuthorUsername: plain, Content: fmt.Sprintf("the other @%s@elsewhere.example", live)},
		{ID: "p3", AuthorUsername: live, Content: "no mentions"},
	}
	s.enrichCustomHandles(posts)

	if got := posts[0].MentionDomains; len(got) != 1 || got[live] != domain {
		t.Errorf("mention_domains = %v, want only %s -> %s (a pending domain isn't live)", got, live, domain)
	}
	if got := posts[1].MentionDomains; got != nil {
		t.Errorf("a Fediverse handle sharing the name got %v, want nothing", got)
	}
	if posts[2].AuthorCustomDomain != domain || posts[2].MentionDomains != nil {
		t.Errorf("author domain %q, mentions %v; the author lookup must still work", posts[2].AuthorCustomDomain, posts[2].MentionDomains)
	}
}
