package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
)

// AGORA-322: peer timeline exchange, in both directions.
//
// The two directions are independent, and the assertions below are mostly about
// keeping them that way. Our subscribing to a peer says nothing about whether
// they carry ours, and conflating them would let one instance's admin silently
// change another's outbound delivery volume.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestPeerTimelineExchange(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()

	mkPeer := func(tag string) string {
		domain := fmt.Sprintf("%s322-%d.example", tag, unique)
		if _, err := db.Exec(`
			INSERT INTO federated_instances (domain, name, instance_url, status, direction)
			VALUES ($1, $2, $3, 'active', 'outbound')
		`, domain, tag, "https://"+domain); err != nil {
			t.Fatalf("seeding peer %s failed: %v", tag, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM federated_instances WHERE domain = $1`, domain) })
		return domain
	}

	t.Run("off by default, including for a peer that already exists", func(t *testing.T) {
		domain := mkPeer("default")
		if s.isTimelineExchangePeer("https://" + domain + "/federation/users/x") {
			t.Error("a newly added peer already carries a timeline; turning this on changes what every local user sees in Explore and is not an upgrade's decision")
		}
		if inboxes := s.peerTimelineInboxes(); contains(inboxes, "https://"+domain+"/federation/inbox") {
			t.Error("a peer that never asked for our timeline is being delivered to")
		}
	})

	t.Run("subscribing to a peer does not make them carry ours", func(t *testing.T) {
		domain := mkPeer("oneway")
		if err := s.SetTimelineExchange(domain, true); err != nil {
			t.Fatalf("SetTimelineExchange: %v", err)
		}
		if !s.isTimelineExchangePeer("https://" + domain + "/federation/users/x") {
			t.Error("the subscription did not take effect, so their posts will not be ingested")
		}
		if inboxes := s.peerTimelineInboxes(); contains(inboxes, "https://"+domain+"/federation/inbox") {
			t.Error("subscribing to a peer's timeline also started sending them ours, which is their decision to make")
		}
	})

	t.Run("a peer carrying ours is not us carrying theirs", func(t *testing.T) {
		domain := mkPeer("theirside")
		db.Exec(`UPDATE federated_instances SET carries_our_timeline = true WHERE domain = $1`, domain)
		if !contains(s.peerTimelineInboxes(), "https://"+domain+"/federation/inbox") {
			t.Error("a peer that subscribed to our timeline is not being delivered to")
		}
		if s.isTimelineExchangePeer("https://" + domain + "/federation/users/x") {
			t.Error("a peer subscribing to us also started pulling their posts in here")
		}
	})

	t.Run("turning it off stops ingestion", func(t *testing.T) {
		domain := mkPeer("offagain")
		if err := s.SetTimelineExchange(domain, true); err != nil {
			t.Fatalf("on: %v", err)
		}
		if err := s.SetTimelineExchange(domain, false); err != nil {
			t.Fatalf("off: %v", err)
		}
		if s.isTimelineExchangePeer("https://" + domain + "/federation/users/x") {
			t.Error("posts are still being ingested after the toggle was turned off")
		}
	})

	t.Run("a blocked instance is refused in both directions", func(t *testing.T) {
		domain := mkPeer("blocked")
		db.Exec(`UPDATE federated_instances SET timeline_exchange = true, carries_our_timeline = true WHERE domain = $1`, domain)
		db.Exec(`INSERT INTO instance_bans (instance) VALUES ($1)`, domain)
		t.Cleanup(func() { db.Exec(`DELETE FROM instance_bans WHERE instance = $1`, domain) })

		if s.isTimelineExchangePeer("https://" + domain + "/federation/users/x") {
			t.Error("a blocked instance's posts are still being ingested")
		}
		if err := s.SetTimelineExchange(domain, true); err == nil {
			t.Error("timeline exchange was enabled for a blocked instance")
		}
	})

	t.Run("an instance we do not peer with cannot be enabled", func(t *testing.T) {
		if err := s.SetTimelineExchange(fmt.Sprintf("stranger322-%d.example", unique), true); err == nil {
			t.Error("timeline exchange was enabled for an instance that is not a peer")
		}
	})
}

// TestEnabledRelayInboxesIncludesPeers covers the fold-in. The three delivery
// sites (Create, Update and Delete) all read this one list, which is what stops
// a peer receiving posts but not their edits.
func TestEnabledRelayInboxesIncludesPeers(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()

	domain := fmt.Sprintf("dual322-%d.example", unique)
	inbox := "https://" + domain + "/federation/inbox"
	db.Exec(`INSERT INTO federated_instances (domain, name, instance_url, status, carries_our_timeline)
	         VALUES ($1, 'Dual', $2, 'active', true)`, domain, "https://"+domain)
	t.Cleanup(func() { db.Exec(`DELETE FROM federated_instances WHERE domain = $1`, domain) })

	if !contains(s.enabledRelayInboxes(), inbox) {
		t.Fatal("a peer carrying our timeline is not in the public-post delivery list")
	}

	// The same host as both a peer and a relay must be delivered to once, not
	// twice: a duplicate is something the receiving end has to absorb.
	db.Exec(`INSERT INTO relays (inbox_url, status) VALUES ($1, 'enabled') ON CONFLICT DO NOTHING`, inbox)
	t.Cleanup(func() { db.Exec(`DELETE FROM relays WHERE inbox_url = $1`, inbox) })

	var n int
	for _, i := range s.enabledRelayInboxes() {
		if i == inbox {
			n++
		}
	}
	if n != 1 {
		t.Errorf("inbox appears %d times, want 1", n)
	}
}

func contains(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}
