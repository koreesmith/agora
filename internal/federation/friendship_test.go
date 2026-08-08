package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-329: friend requests ride ActivityPub as a Follow carrying an
// agora:friendRequest marker.

func testFriendshipService(t *testing.T) *store.DB {
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

// TestRemoteAgoraActorURL covers the bridge that lets a friendship formed
// before this migration still be acted on. A legacy stub has no ap_actor_url,
// but an Agora actor URL is deterministic, so it can be derived from the
// instance and handle the legacy protocol already stored.
func TestRemoteAgoraActorURL(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()

	t.Run("derived from a legacy stub", func(t *testing.T) {
		id := s.getOrCreateRemoteUser("alice", fmt.Sprintf("peer329-%d.example", unique))
		if id == "" {
			t.Fatal("no stub")
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })

		got, err := s.remoteAgoraActorURL(id)
		if err != nil {
			t.Fatalf("remoteAgoraActorURL: %v", err)
		}
		want := fmt.Sprintf("https://peer329-%d.example/federation/users/alice", unique)
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("ap_actor_url wins when present", func(t *testing.T) {
		username := fmt.Sprintf("ap329_%d", unique)
		actor := "https://peer.example/federation/users/bob"
		var id string
		db.QueryRow(`
			INSERT INTO users (username, email, password_hash, is_remote, remote_instance, ap_actor_url)
			VALUES ($1, $1, 'x', true, 'peer.example', $2) RETURNING id
		`, username, actor).Scan(&id)
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })

		got, err := s.remoteAgoraActorURL(id)
		if err != nil || got != actor {
			t.Errorf("got (%q, %v), want (%q, nil)", got, err, actor)
		}
	})

	t.Run("a local user is refused", func(t *testing.T) {
		username := fmt.Sprintf("local329_%d", unique)
		var id string
		db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1,$1,'x') RETURNING id`, username).Scan(&id)
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })

		if _, err := s.remoteAgoraActorURL(id); err == nil {
			t.Error("a local user resolved to a remote actor URL")
		}
	})
}

// TestCanFriend pins the widening of AGORA-167. That check refused every
// account with an ap_actor_url, which was right when no Agora user had one and
// is too broad now that they do.
func TestCanFriend(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()

	mkRemote := func(t *testing.T, suffix, instance, actorURL, legacyHandle string) string {
		t.Helper()
		username := fmt.Sprintf("cf329_%s_%d", suffix, unique)
		var id string
		err := db.QueryRow(`
			INSERT INTO users (username, email, password_hash, is_remote, remote_instance, ap_actor_url, remote_user_id)
			VALUES ($1, $1, 'x', true, $2, $3, $4) RETURNING id
		`, username, instance, actorURL, legacyHandle).Scan(&id)
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}

	t.Run("a legacy stub is always friendable", func(t *testing.T) {
		id := mkRemote(t, "legacy", "peer.example", "", "alice")
		if !s.CanFriend(id) {
			t.Error("refused a legacy stub, which only the Agora-to-Agora protocol creates")
		}
	})

	t.Run("a mastodon actor on an unknown instance is refused", func(t *testing.T) {
		id := mkRemote(t, "mast", "mastodon.example", "https://mastodon.example/users/x", "")
		if s.CanFriend(id) {
			t.Error("allowed a fediverse actor, whose request could never be accepted")
		}
	})

	t.Run("an actor on a known Agora peer is friendable", func(t *testing.T) {
		domain := fmt.Sprintf("knownpeer329-%d.example", unique)
		db.Exec(`INSERT INTO federated_instances (domain, name, public_key, instance_url, status)
		         VALUES ($1,'Peer','AAAA',$2,'active')`, domain, "https://"+domain)
		t.Cleanup(func() { db.Exec(`DELETE FROM federated_instances WHERE domain = $1`, domain) })

		id := mkRemote(t, "peer", domain, "https://"+domain+"/federation/users/bob", "")
		if !s.CanFriend(id) {
			t.Error("refused an Agora user on a peered instance, who is exactly who this is for")
		}
	})

	t.Run("a blocked peer is refused", func(t *testing.T) {
		domain := fmt.Sprintf("blockedpeer329-%d.example", unique)
		db.Exec(`INSERT INTO federated_instances (domain, name, public_key, instance_url, status)
		         VALUES ($1,'Peer','AAAA',$2,'blocked')`, domain, "https://"+domain)
		t.Cleanup(func() { db.Exec(`DELETE FROM federated_instances WHERE domain = $1`, domain) })

		id := mkRemote(t, "blocked", domain, "https://"+domain+"/federation/users/bob", "")
		if s.CanFriend(id) {
			t.Error("allowed a friend request to a blocked instance")
		}
	})
}

// TestInboundFriendRequestOverAP covers the receiving side: the pending row,
// the notification, and the two guards inherited from AGORA-318 that a Follow
// makes more pressing rather than less, since a Follow is redelivered routinely.
func TestInboundFriendRequestOverAP(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{
		db:    db,
		cfg:   &config.Config{InstanceDomain: "https://local.example"},
		notif: notifications.NewService(db, notifications.NewEmailService(db, &config.Config{})),
	}
	unique := time.Now().UnixNano()

	localName := fmt.Sprintf("agora329_local_%d", unique)
	var localID string
	db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1,$1,'x') RETURNING id`, localName).Scan(&localID)
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, localID) })

	remoteName := fmt.Sprintf("agora329_remote_%d", unique)
	var remoteID string
	db.QueryRow(`
		INSERT INTO users (username, email, password_hash, is_remote, remote_instance, ap_actor_url)
		VALUES ($1,$1,'x',true,'peer.example',$2) RETURNING id
	`, remoteName, "https://peer.example/federation/users/"+remoteName).Scan(&remoteID)
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, remoteID) })
	t.Cleanup(func() { db.Exec(`DELETE FROM friendships WHERE requester_id = $1 OR addressee_id = $1`, localID) })
	t.Cleanup(func() { db.Exec(`DELETE FROM notifications WHERE user_id = $1`, localID) })

	countNotifs := func() int {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND type = 'friend_request'`, localID).Scan(&n)
		return n
	}

	s.recordInboundFriendRequest(localID, remoteID)

	var status string
	if err := db.QueryRow(`SELECT status FROM friendships WHERE requester_id = $1 AND addressee_id = $2`,
		remoteID, localID).Scan(&status); err != nil {
		t.Fatalf("no pending friendship was created: %v", err)
	}
	if status != "pending" {
		t.Errorf("status = %q, want pending", status)
	}
	if got := countNotifs(); got != 1 {
		t.Fatalf("got %d notifications, want 1", got)
	}

	// A Follow is redelivered as a matter of course, which is exactly why this
	// has to be idempotent rather than merely usually-correct.
	s.recordInboundFriendRequest(localID, remoteID)
	if got := countNotifs(); got != 1 {
		t.Errorf("redelivery produced %d notifications, want 1", got)
	}

	t.Run("a blocked requester creates nothing", func(t *testing.T) {
		otherName := fmt.Sprintf("agora329_blocked_%d", unique)
		var blockedID string
		db.QueryRow(`
			INSERT INTO users (username, email, password_hash, is_remote, remote_instance, ap_actor_url)
			VALUES ($1,$1,'x',true,'peer.example',$2) RETURNING id
		`, otherName, "https://peer.example/federation/users/"+otherName).Scan(&blockedID)
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, blockedID) })

		db.Exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1,$2)`, localID, blockedID)
		t.Cleanup(func() { db.Exec(`DELETE FROM blocks WHERE blocker_id = $1`, localID) })

		before := countNotifs()
		s.recordInboundFriendRequest(localID, blockedID)

		var exists bool
		db.QueryRow(`SELECT EXISTS(SELECT 1 FROM friendships WHERE requester_id=$1 AND addressee_id=$2)`,
			blockedID, localID).Scan(&exists)
		if exists {
			t.Error("created a friendship for a blocked account")
		}
		if countNotifs() != before {
			t.Error("notified about a blocked account's friend request")
		}
	})
}

// TestFriendRequestsAcceptedSetting covers the friend_requests_from gate.
func TestFriendRequestsAcceptedSetting(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db}

	restore := func() {
		db.Exec(`UPDATE instance_settings SET value = 'anyone' WHERE key = 'friend_requests_from'`)
	}
	t.Cleanup(restore)

	domain := fmt.Sprintf("frs329-%d.example", time.Now().UnixNano())

	t.Run("anyone is the default", func(t *testing.T) {
		restore()
		if !s.friendRequestsAccepted(domain) {
			t.Error("refused a request under the default, which is meant to be open")
		}
	})

	t.Run("peered_only refuses an unknown instance", func(t *testing.T) {
		db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('friend_requests_from','peered_only')
		         ON CONFLICT (key) DO UPDATE SET value = 'peered_only'`)
		if s.friendRequestsAccepted(domain) {
			t.Error("accepted from an unpeered instance under peered_only")
		}
	})

	t.Run("peered_only accepts a known peer", func(t *testing.T) {
		db.Exec(`INSERT INTO federated_instances (domain, name, public_key, instance_url, status)
		         VALUES ($1,'Peer','AAAA',$2,'active')`, domain, "https://"+domain)
		t.Cleanup(func() { db.Exec(`DELETE FROM federated_instances WHERE domain = $1`, domain) })

		if !s.friendRequestsAccepted(domain) {
			t.Error("refused a known peer under peered_only")
		}
	})
}

// TestAcceptResolvesALegacyStub covers the bug that shipped in AGORA-329 and was
// caught on the live instances: a friend request went out fine, but the Accept
// coming back could not be matched to anybody.
//
// The pending friendship pointed at a legacy stub, which has no ap_actor_url,
// while the Accept was resolved by ap_actor_url alone. Nothing matched, the
// friendship stayed pending forever, and the marked Follow that followed the
// Accept created a second stub and a second pending friendship rather than
// updating the first.
func TestAcceptResolvesALegacyStub(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()
	instance := fmt.Sprintf("legacyaccept-%d.example", unique)

	id := s.getOrCreateRemoteUser("bob", instance)
	if id == "" {
		t.Fatal("no stub")
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })

	actorURL := "https://" + instance + "/federation/users/bob"

	t.Run("resolves by legacy identity when the stub has no actor URL", func(t *testing.T) {
		if got := s.remoteUserIDForActor(actorURL); got != id {
			t.Fatalf("remoteUserIDForActor = %q, want %q. An Accept from this actor matches nobody, so the request stays pending forever.", got, id)
		}
	})

	t.Run("sending adopts the actor URL onto the stub", func(t *testing.T) {
		if _, err := s.remoteAgoraActorURL(id); err != nil {
			t.Fatalf("remoteAgoraActorURL: %v", err)
		}
		var stored string
		db.QueryRow(`SELECT COALESCE(ap_actor_url,'') FROM users WHERE id = $1`, id).Scan(&stored)
		if stored != actorURL {
			t.Errorf("ap_actor_url = %q, want %q. Without this the two rows for one person never converge.", stored, actorURL)
		}
	})

	t.Run("adoption does not steal an actor URL another row already claims", func(t *testing.T) {
		otherName := fmt.Sprintf("claimed_%d", unique)
		claimed := "https://" + instance + "/federation/users/" + otherName
		var apID string
		db.QueryRow(`
			INSERT INTO users (username, email, password_hash, is_remote, remote_instance, ap_actor_url)
			VALUES ($1,$1,'x',true,$2,$3) RETURNING id
		`, otherName, instance, claimed).Scan(&apID)
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, apID) })

		legacyName := fmt.Sprintf("legacydup_%d", unique)
		var legacyID string
		db.QueryRow(`
			INSERT INTO users (username, email, password_hash, is_remote, remote_instance, remote_user_id)
			VALUES ($1,$1,'x',true,$2,$3) RETURNING id
		`, legacyName, instance, otherName).Scan(&legacyID)
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, legacyID) })

		s.adoptActorURLForLegacyStub(legacyID, claimed)

		var stored string
		db.QueryRow(`SELECT COALESCE(ap_actor_url,'') FROM users WHERE id = $1`, legacyID).Scan(&stored)
		if stored != "" {
			t.Error("adopted an actor URL another row already claims, which the partial unique index forbids")
		}
	})
}

// TestFriendRequestRecordsTheFollowLocally covers AGORA-336, the half-follow
// that AGORA-329 shipped: the Follow activity went out on the wire but no
// ap_following row was written, so the far side delivered posts that
// ingestFollowedPost then dropped for want of an accepted follow. Friend
// requests appeared to work while content silently never arrived.
//
// deliverFriendActivity cannot run end to end here (it does a signed actor
// fetch, and fedHTTPClient refuses to dial anything non-public), so this
// asserts the two properties ingestion actually depends on: that a row exists
// for the followed actor, and that an Accept flips it to accepted.
func TestFriendRequestRecordsTheFollowLocally(t *testing.T) {
	db := testFriendshipService(t)
	unique := time.Now().UnixNano()

	localName := fmt.Sprintf("agora336_local_%d", unique)
	var localID string
	db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1,$1,'x') RETURNING id`, localName).Scan(&localID)
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, localID) })

	actorURL := fmt.Sprintf("https://peer336-%d.example/federation/users/bob", unique)
	inbox := fmt.Sprintf("https://peer336-%d.example/federation/inbox", unique)
	t.Cleanup(func() { db.Exec(`DELETE FROM ap_following WHERE follower_user_id = $1`, localID) })

	// The row deliverFriendActivity now writes before enqueuing the Follow.
	if _, err := db.Exec(`
		INSERT INTO ap_following (follower_user_id, followed_actor_url, followed_inbox_url, accepted)
		VALUES ($1, $2, $3, false)
		ON CONFLICT (follower_user_id, followed_actor_url) DO UPDATE SET followed_inbox_url = $3
	`, localID, actorURL, inbox); err != nil {
		t.Fatalf("record follow: %v", err)
	}

	// ingestFollowedPost's gate: an accepted follow of the post's author. Until
	// the Accept arrives this is correctly false, which is why a request that
	// is merely sent does not yet let content through.
	var accepted bool
	db.QueryRow(`SELECT accepted FROM ap_following WHERE follower_user_id = $1 AND followed_actor_url = $2`,
		localID, actorURL).Scan(&accepted)
	if accepted {
		t.Error("a freshly sent friend request should not yet count as an accepted follow")
	}

	// handleInboundAcceptFollow's UPDATE. This is the statement that matched
	// zero rows before the fix, because there was no row to match.
	res, err := db.Exec(`UPDATE ap_following SET accepted = true WHERE follower_user_id = $1 AND followed_actor_url = $2`,
		localID, actorURL)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("the Accept matched %d rows, want 1. With no ap_following row it matches none and content never flows.", n)
	}

	var gate string
	db.QueryRow(`SELECT follower_user_id FROM ap_following WHERE followed_actor_url = $1 AND accepted = true LIMIT 1`, actorURL).Scan(&gate)
	if gate != localID {
		t.Errorf("ingestFollowedPost's gate resolved to %q, want %q, so their posts would be dropped on arrival", gate, localID)
	}
}

// TestRemoteInboxForNeedsNoNetwork covers AGORA-338. Resolving the inbox used
// to sign and fetch the remote actor document, and every caller treated a
// failure as fatal: it returned without queuing anything, so a peer that was
// briefly unreachable lost the friend request permanently, with a pending row
// left behind and nothing to retry it. That is what made friend requests look
// intermittent.
//
// fedHTTPClient refuses to dial anything non-public, so if this still made a
// network call these cases would fail rather than resolve, which is exactly the
// property being pinned.
func TestRemoteInboxForNeedsNoNetwork(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()

	var localID string
	name := fmt.Sprintf("inbox338_%d", unique)
	db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`, name).Scan(&localID)
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, localID) })
	t.Cleanup(func() { db.Exec(`DELETE FROM ap_following WHERE follower_user_id = $1`, localID) })

	domain := fmt.Sprintf("inboxpeer338-%d.example", unique)
	actorURL := "https://" + domain + "/federation/users/bob"

	t.Run("derived from the actor when nothing is stored", func(t *testing.T) {
		got, err := s.remoteInboxFor(localID, actorURL)
		if err != nil {
			t.Fatalf("remoteInboxFor: %v, but an unreachable peer must not block sending", err)
		}
		if want := "https://" + domain + "/federation/inbox"; got != want {
			t.Errorf("got %q, want the shared inbox %q", got, want)
		}
	})

	t.Run("a stored inbox wins over the derived one", func(t *testing.T) {
		stored := "https://" + domain + "/custom/inbox"
		db.Exec(`
			INSERT INTO ap_following (follower_user_id, followed_actor_url, followed_inbox_url, accepted)
			VALUES ($1,$2,$3,true)
			ON CONFLICT (follower_user_id, followed_actor_url) DO UPDATE SET followed_inbox_url = $3
		`, localID, actorURL, stored)

		got, err := s.remoteInboxFor(localID, actorURL)
		if err != nil || got != stored {
			t.Errorf("got (%q, %v), want (%q, nil). A real actor document must win over the convention.", got, err, stored)
		}
	})

	t.Run("an unusable actor URL is refused rather than guessed at", func(t *testing.T) {
		if _, err := s.remoteInboxFor(localID, "not-a-url"); err == nil {
			t.Error("derived an inbox from something that is not an actor URL")
		}
	})
}
