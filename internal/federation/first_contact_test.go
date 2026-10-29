package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-314/321: an instance nobody had heard of used to register itself as a
// live peer purely as a side effect of getRemotePublicKey fetching its key to
// verify an inbound activity, with no notification and nothing distinguishing
// it in the admin list from a peer an admin had deliberately added.
//
// The upsert now reports whether it inserted, via RETURNING (xmax = 0), and
// that one boolean carries both features: it decides whether to notify, and it
// is what makes "they connected to you" a fact rather than a guess. It is also
// the only thing standing between one notification and one per delivered
// activity, since a peer sends many and each reaches this path until its key is
// cached. Worth its own test for the same reason recordAPFollower's equivalent
// check has one (AGORA-313).
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestFirstContactIsReportedOnceAndNotifiesOnlyAdmins(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db, notif: notifications.NewService(db, notifications.NewEmailService(db, &config.Config{}))}

	domain := fmt.Sprintf("agora314-%d.example", time.Now().UnixNano())
	t.Cleanup(func() { db.Exec(`DELETE FROM federated_instances WHERE domain = $1`, domain) })
	t.Cleanup(func() { db.Exec(`DELETE FROM notifications WHERE type = 'federation_request' AND data = $1`, domain) })

	// Mirrors getRemotePublicKey's upsert. Kept in step with it by the
	// direction assertions below, which fail if the CASE arms drift.
	upsert := func() bool {
		var first bool
		db.QueryRow(`
			INSERT INTO federated_instances (domain, name, public_key, instance_url, status, direction)
			VALUES ($1, 'Peer', 'AAAA', $2, 'active', 'inbound')
			ON CONFLICT (domain) DO UPDATE
			  SET public_key   = 'AAAA',
			      last_seen_at = NOW(),
			      direction    = CASE WHEN federated_instances.direction = 'outbound' THEN 'mutual'
			                          ELSE federated_instances.direction END
			RETURNING (xmax = 0)
		`, domain, "https://"+domain).Scan(&first)
		return first
	}

	if !upsert() {
		t.Fatal("the insert did not report itself as first contact, so no admin would ever be told")
	}
	s.notifyAdminsOfFederationRequest(domain)

	if upsert() {
		t.Error("a second delivery reported first contact again, so a peer's normal traffic would notify repeatedly")
	}

	var admins int
	db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin' AND deletion_scheduled_at IS NULL`).Scan(&admins)
	if admins == 0 {
		t.Fatal("no admin accounts in the test database, cannot verify the fan-out")
	}

	var notified int
	db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE type = 'federation_request' AND data = $1`, domain).Scan(&notified)
	if notified != admins {
		t.Errorf("got %d notifications for %d admins, want one each", notified, admins)
	}

	// Deliberately narrower than moderation's notifyAdmins, which includes
	// moderators: the federation routes are behind RequireAdmin, so a notified
	// moderator would have nothing to click.
	var nonAdmins int
	db.QueryRow(`
		SELECT COUNT(*) FROM notifications n JOIN users u ON u.id = n.user_id
		WHERE n.type = 'federation_request' AND n.data = $1 AND u.role <> 'admin'
	`, domain).Scan(&nonAdmins)
	if nonAdmins != 0 {
		t.Errorf("%d non-admin(s) were notified about a federation request they cannot act on", nonAdmins)
	}

	var direction string
	db.QueryRow(`SELECT direction FROM federated_instances WHERE domain = $1`, domain).Scan(&direction)
	if direction != "inbound" {
		t.Errorf("direction = %q, want inbound for an instance that contacted us first", direction)
	}

	t.Run("an outbound peering they then contact becomes mutual", func(t *testing.T) {
		if _, err := db.Exec(`UPDATE federated_instances SET direction = 'outbound' WHERE domain = $1`, domain); err != nil {
			t.Fatalf("set outbound: %v", err)
		}
		upsert()

		var d string
		db.QueryRow(`SELECT direction FROM federated_instances WHERE domain = $1`, domain).Scan(&d)
		if d != "mutual" {
			t.Errorf("direction = %q, want mutual once an instance we added contacts us back", d)
		}
	})

	t.Run("an unknown-origin peering is not upgraded to a claim", func(t *testing.T) {
		if _, err := db.Exec(`UPDATE federated_instances SET direction = 'unknown' WHERE domain = $1`, domain); err != nil {
			t.Fatalf("set unknown: %v", err)
		}
		upsert()

		var d string
		db.QueryRow(`SELECT direction FROM federated_instances WHERE domain = $1`, domain).Scan(&d)
		if d != "unknown" {
			t.Errorf("direction = %q, want unknown: inbound traffic proves they are talking to us, not that they started it", d)
		}
	})
}
