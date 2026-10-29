package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
)

// AGORA-330: first-contact registration used to be a side effect of fetching a
// peer's legacy signing key. Deleting that transport would have taken it along,
// and with it the Federation tab's inbound direction, the admin notification,
// and the peered check that CanFriend and friend_requests_from both read. It is
// deliberate now, so its guards are worth pinning: each one prevents a row that
// would misrepresent who this instance federates with.
func TestRegisterInboundPeerGuards(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()

	count := func(domain string) int {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM federated_instances WHERE LOWER(domain) = LOWER($1)`, domain).Scan(&n)
		return n
	}

	t.Run("our own domain is never recorded as a peer", func(t *testing.T) {
		s.registerInboundPeer("local.example")
		if count("local.example") != 0 {
			db.Exec(`DELETE FROM federated_instances WHERE domain = 'local.example'`)
			t.Error("this instance registered itself as its own peer")
		}
	})

	t.Run("a blocked instance is not registered", func(t *testing.T) {
		domain := fmt.Sprintf("blocked330-%d.example", unique)
		db.Exec(`INSERT INTO instance_bans (instance) VALUES ($1)`, domain)
		t.Cleanup(func() { db.Exec(`DELETE FROM instance_bans WHERE instance = $1`, domain) })
		t.Cleanup(func() { db.Exec(`DELETE FROM federated_instances WHERE domain = $1`, domain) })

		s.registerInboundPeer(domain)
		if count(domain) != 0 {
			t.Error("a blocked instance was added to the peer list, which would show an admin they federate with somebody they have banned")
		}
	})

	t.Run("an existing peer is refreshed without a network call", func(t *testing.T) {
		domain := fmt.Sprintf("known330-%d.example", unique)
		// A domain that resolves nowhere: if this reached FetchInstanceInfo the
		// fetch would fail and last_seen_at would not move, so the assertion
		// below is what proves the short-circuit.
		db.Exec(`
			INSERT INTO federated_instances (domain, name, instance_url, status, direction, last_seen_at)
			VALUES ($1, 'Known', $2, 'active', 'outbound', NOW() - INTERVAL '7 days')
		`, domain, "https://"+domain)
		t.Cleanup(func() { db.Exec(`DELETE FROM federated_instances WHERE domain = $1`, domain) })

		var before time.Time
		db.QueryRow(`SELECT last_seen_at FROM federated_instances WHERE domain = $1`, domain).Scan(&before)

		s.registerInboundPeer(domain)

		var after time.Time
		var direction string
		db.QueryRow(`SELECT last_seen_at, direction FROM federated_instances WHERE domain = $1`, domain).Scan(&after, &direction)
		if !after.After(before) {
			t.Error("last_seen_at did not move, so an existing peer's liveness is not being refreshed")
		}
		if direction != "outbound" {
			t.Errorf("direction = %q, want %q; contact from a peer we added must not rewrite how the peering started", direction, "outbound")
		}
		if count(domain) != 1 {
			t.Errorf("got %d rows for %s, want 1", count(domain), domain)
		}
	})

	t.Run("an empty domain is ignored", func(t *testing.T) {
		var before int
		db.QueryRow(`SELECT COUNT(*) FROM federated_instances`).Scan(&before)
		s.registerInboundPeer("")
		s.registerInboundPeer("   ")
		var after int
		db.QueryRow(`SELECT COUNT(*) FROM federated_instances`).Scan(&after)
		if after != before {
			t.Error("a blank domain created a peer row")
		}
	})
}
