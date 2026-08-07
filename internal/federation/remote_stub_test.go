package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-317: users.profile_private defaults to TRUE, which is right for a
// local signup and wrong for a stub standing in for a remote account.
// getOrCreateRemoteUser never set it, so every account cached from a federated
// Agora instance was created private, and PublicFeed filters on
// `NOT u.profile_private`, so posts ingested from a peer were stored and then
// never shown to anyone. AGORA-164 fixed the identical defect on the
// ActivityPub stub path; its scoping (ap_actor_url != '') meant it never
// reached these.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestRemoteStubIsNotCreatedPrivate(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}

	unique := time.Now().UnixNano()
	instance := fmt.Sprintf("agora317-%d.example", unique)
	handle := "carol"

	id := s.getOrCreateRemoteUser(handle, instance)
	if id == "" {
		t.Fatal("getOrCreateRemoteUser returned no id")
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })

	var private bool
	if err := db.QueryRow(`SELECT profile_private FROM users WHERE id = $1`, id).Scan(&private); err != nil {
		t.Fatalf("read back stub: %v", err)
	}
	if private {
		t.Error("remote stub was created private, so nothing it authors would ever appear in a feed")
	}

	t.Run("a stub created private before the fix is repaired on next contact", func(t *testing.T) {
		if _, err := db.Exec(`UPDATE users SET profile_private = true WHERE id = $1`, id); err != nil {
			t.Fatalf("force private: %v", err)
		}

		// A second sighting of the same account takes the early-return path
		// (the row already exists), so the repair has to come from the
		// migration backfill rather than the upsert. Verify the backfill's
		// predicate matches this row.
		var wouldRepair bool
		db.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM users
				WHERE id = $1 AND is_remote = true AND profile_private = true
				  AND remote_user_id != '' AND ap_actor_url = '' AND atproto_remote_did = ''
			)`, id).Scan(&wouldRepair)
		if !wouldRepair {
			t.Error("the AGORA-317 backfill predicate does not match a legacy stub it is supposed to repair")
		}
	})
}

// TestStubBackfillLeavesOtherAccountTypesAlone is the other half of the
// backfill's correctness: it must not reach a local account, an ActivityPub
// stub, or a Bluesky one. All three remote sources populate remote_instance,
// which is why the predicate keys off remote_user_id instead.
func TestStubBackfillLeavesOtherAccountTypesAlone(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	unique := time.Now().UnixNano()

	matches := func(id string) bool {
		var m bool
		db.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM users
				WHERE id = $1 AND is_remote = true AND profile_private = true
				  AND remote_user_id != '' AND ap_actor_url = '' AND atproto_remote_did = ''
			)`, id).Scan(&m)
		return m
	}

	// Each case supplies its own full argument list, keyed off $1 = username,
	// so no case has to know about another's parameters.
	cases := []struct {
		name   string
		insert string
		extra  []any
	}{
		{
			name: "local account",
			insert: `INSERT INTO users (username, email, password_hash, profile_private, is_remote)
			         VALUES ($1, $1, 'x', true, false) RETURNING id`,
		},
		{
			name: "activitypub stub",
			insert: `INSERT INTO users (username, email, password_hash, profile_private, is_remote, remote_instance, ap_actor_url)
			         VALUES ($1, $1, 'x', true, true, 'mastodon.example', $2) RETURNING id`,
			extra: []any{fmt.Sprintf("https://mastodon.example/users/agora317_%d", unique)},
		},
		{
			name: "bluesky stub",
			insert: `INSERT INTO users (username, email, password_hash, profile_private, is_remote, remote_instance, atproto_remote_did)
			         VALUES ($1, $1, 'x', true, true, 'bsky.app', $2) RETURNING id`,
			extra: []any{fmt.Sprintf("did:plc:agora317%d", unique)},
		},
	}

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			username := fmt.Sprintf("agora317_scope_%d_%d", unique, i)

			var id string
			if err := db.QueryRow(c.insert, append([]any{username}, c.extra...)...).Scan(&id); err != nil {
				t.Fatalf("insert: %v", err)
			}
			t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })

			if matches(id) {
				t.Errorf("the AGORA-317 backfill would flip profile_private on a %s, which it must not touch", c.name)
			}
		})
	}
}
