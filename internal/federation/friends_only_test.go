package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
)

// AGORA-337: friends-only posts now federate. The stored visibility is decided
// from what the sender claims plus what the addressing actually shows, and
// getting that wrong publishes somebody's private post, so it is pinned here
// rather than left to a reading of the call site.

func TestFriendsOnlyVisibilityRequiresBothClaimAndAddressing(t *testing.T) {
	cases := []struct {
		name              string
		marker            string
		addressedPublicly bool
		want              string
	}{
		{"friends-only and not public stores as friends", "friends", false, "friends"},
		{"no marker stores public, as every other fediverse post does", "", false, "public"},
		{"an unknown marker is not trusted into a private visibility", "close-friends", false, "public"},
		// The contradiction case. A Note claiming friends-only while addressed
		// to Public has already been seen by the sender's followers, so
		// treating it as private here would hide it from the very people who
		// can see it anyway, while implying a privacy that was never real.
		{"friends-only claimed but addressed publicly stores public", "friends", true, "public"},
		{"no marker and public stays public", "", true, "public"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := friendsOnlyVisibility(c.marker, c.addressedPublicly); got != c.want {
				t.Errorf("friendsOnlyVisibility(%q, %v) = %q, want %q", c.marker, c.addressedPublicly, got, c.want)
			}
		})
	}
}

func TestIsAddressedPublicly(t *testing.T) {
	const public = "https://www.w3.org/ns/activitystreams#Public"

	cases := []struct {
		name string
		to   []string
		cc   []string
		want bool
	}{
		{"named actors only", []string{"https://a.example/users/x"}, []string{}, false},
		{"public in to", []string{public}, []string{}, true},
		{"public in cc", []string{"https://a.example/users/x"}, []string{public}, true},
		// Mastodon and others use these shorthands interchangeably, and missing
		// one would mean reading a public post as private.
		{"as:Public shorthand", []string{"as:Public"}, []string{}, true},
		{"bare Public shorthand", []string{}, []string{"Public"}, true},
		{"empty addressing", nil, nil, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isAddressedPublicly(c.to, c.cc); got != c.want {
				t.Errorf("isAddressedPublicly(%v, %v) = %v, want %v", c.to, c.cc, got, c.want)
			}
		})
	}
}

// TestRemoteFriendRecipients covers who a friends-only post is addressed to:
// accepted friends on other Agora instances, and nobody else. A local friend
// needs no delivery, and a fediverse account is not a friend.
func TestRemoteFriendRecipients(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()

	var localID string
	localName := fmt.Sprintf("fo337_local_%d", unique)
	db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`, localName).Scan(&localID)
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, localID) })
	t.Cleanup(func() { db.Exec(`DELETE FROM friendships WHERE requester_id = $1 OR addressee_id = $1`, localID) })

	mk := func(suffix, instance, actorURL string, remote bool) string {
		name := fmt.Sprintf("fo337_%s_%d", suffix, unique)
		var id string
		db.QueryRow(`
			INSERT INTO users (username,email,password_hash,is_remote,remote_instance,ap_actor_url)
			VALUES ($1,$1,'x',$2,$3,$4) RETURNING id
		`, name, remote, instance, actorURL).Scan(&id)
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	befriend := func(otherID, status string) {
		db.Exec(`INSERT INTO friendships (requester_id,addressee_id,status) VALUES ($1,$2,$3)`, localID, otherID, status)
	}

	peerDomain := fmt.Sprintf("fopeer337-%d.example", unique)
	remoteFriend := mk("remotefriend", peerDomain, "https://"+peerDomain+"/federation/users/bob", true)
	localFriend := mk("localfriend", "", "", false)
	remotePending := mk("pending", peerDomain, "https://"+peerDomain+"/federation/users/carol", true)

	befriend(remoteFriend, "accepted")
	befriend(localFriend, "accepted")
	befriend(remotePending, "pending")

	got := s.remoteFriendRecipients(localID)

	if len(got) != 1 {
		t.Fatalf("got %d recipients, want 1 (only the accepted remote friend)", len(got))
	}
	if got[0].actorURL != "https://"+peerDomain+"/federation/users/bob" {
		t.Errorf("actorURL = %q, want the accepted remote friend's", got[0].actorURL)
	}
	// No ap_following row was seeded, so this exercises the shared-inbox
	// fallback that keeps friendships predating AGORA-336 deliverable.
	if want := "https://" + peerDomain + "/federation/inbox"; got[0].inboxURL != want {
		t.Errorf("inboxURL = %q, want the derived shared inbox %q", got[0].inboxURL, want)
	}
}

// TestIsAcceptedFriendByActor covers the gate AGORA-339 put in front of replies
// and reactions on a friends-only thread. Getting it wrong either lets a
// stranger into a private conversation or locks a genuine friend out of one, so
// both directions are pinned.
func TestIsAcceptedFriendByActor(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()

	var authorID string
	authorName := fmt.Sprintf("a339%d", unique%1_000_000)
	db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`, authorName).Scan(&authorID)
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })
	t.Cleanup(func() { db.Exec(`DELETE FROM friendships WHERE requester_id = $1 OR addressee_id = $1`, authorID) })

	// users.username is VARCHAR(50), so these stay short. An over-long name
	// makes the insert fail silently and the test then asserts against a
	// friendship that was never created, which reads as a code bug.
	domain := fmt.Sprintf("p339-%d.example", unique%1_000_000)
	mkRemote := func(handle string, useActorURL bool) (string, string) {
		t.Helper()
		actorURL := "https://" + domain + "/federation/users/" + handle
		name := fmt.Sprintf("%s339%d", handle, unique%1_000_000)
		var id string
		var err error
		if useActorURL {
			err = db.QueryRow(`INSERT INTO users (username,email,password_hash,is_remote,remote_instance,ap_actor_url)
			             VALUES ($1,$1,'x',true,$2,$3) RETURNING id`, name, domain, actorURL).Scan(&id)
		} else {
			// A legacy stub: no ap_actor_url at all. A friendship formed before
			// AGORA-333 looks like this, and must still pass.
			err = db.QueryRow(`INSERT INTO users (username,email,password_hash,is_remote,remote_instance,remote_user_id)
			             VALUES ($1,$1,'x',true,$2,$3) RETURNING id`, name, domain, handle).Scan(&id)
		}
		if err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id, actorURL
	}
	befriend := func(otherID, status string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO friendships (requester_id,addressee_id,status) VALUES ($1,$2,$3)`,
			authorID, otherID, status); err != nil {
			t.Fatalf("befriend: %v", err)
		}
	}

	friendID, friendActor := mkRemote("friend", true)
	befriend(friendID, "accepted")

	_, strangerActor := mkRemote("stranger", true)

	pendingID, pendingActor := mkRemote("pending", true)
	befriend(pendingID, "pending")

	legacyID, legacyActor := mkRemote("legacy", false)
	befriend(legacyID, "accepted")

	cases := []struct {
		name  string
		actor string
		want  bool
	}{
		{"an accepted friend may join the thread", friendActor, true},
		{"a stranger may not", strangerActor, false},
		{"a pending request is not yet a friend", pendingActor, false},
		// Resolved through remoteUserIDForActor, so a friendship recorded
		// against a legacy stub with no ap_actor_url still counts.
		{"a friend on a legacy stub still counts", legacyActor, true},
		{"an unknown actor may not", "https://" + domain + "/federation/users/nobody", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := s.isAcceptedFriendByActor(authorID, c.actor); got != c.want {
				t.Errorf("isAcceptedFriendByActor(author, %q) = %v, want %v", c.actor, got, c.want)
			}
		})
	}
}

// TestIsThreadOwnerForwarding covers the one exception AGORA-340 makes to the
// rule that a signer may only speak for itself. Every condition is a boundary
// worth pinning: too loose and a peer can forge a reply from anyone, too tight
// and a limited-audience conversation cannot be completed at all.
func TestIsThreadOwnerForwarding(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano() % 1_000_000

	domain := fmt.Sprintf("t340-%d.example", unique)
	ownerActor := "https://" + domain + "/federation/users/alice"
	otherActor := "https://" + domain + "/federation/users/mallory"

	// The thread author, as this instance holds them: a remote stub.
	var ownerID string
	if err := db.QueryRow(`
		INSERT INTO users (username,email,password_hash,is_remote,remote_instance,ap_actor_url)
		VALUES ($1,$1,'x',true,$2,$3) RETURNING id
	`, fmt.Sprintf("own340%d", unique), domain, ownerActor).Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, ownerID) })

	mkPost := func(visibility string) string {
		t.Helper()
		remoteID := fmt.Sprintf("https://%s/federation/users/alice/posts/%s-%d", domain, visibility, unique)
		var id string
		if err := db.QueryRow(`
			INSERT INTO posts (author_id, content, visibility, is_remote, remote_post_id, remote_instance)
			VALUES ($1,'hi',$2,true,$3,$4) RETURNING id
		`, ownerID, visibility, remoteID, domain).Scan(&id); err != nil {
			t.Fatalf("insert post: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, id) })
		return remoteID
	}

	friendsThread := mkPost("friends")
	publicThread := mkPost("public")

	t.Run("the thread's own author may forward into it", func(t *testing.T) {
		if !s.isThreadOwnerForwarding(ownerActor, friendsThread) {
			t.Error("refused the thread owner, so a limited-audience conversation can never be completed")
		}
	})

	t.Run("nobody else may", func(t *testing.T) {
		if s.isThreadOwnerForwarding(otherActor, friendsThread) {
			t.Error("allowed a non-owner to forward, which lets any peer forge a reply from anyone")
		}
	})

	t.Run("a public thread grants no bypass", func(t *testing.T) {
		// A public thread needs no forwarding, so it must not acquire an
		// exception it has no use for.
		if s.isThreadOwnerForwarding(ownerActor, publicThread) {
			t.Error("allowed forwarding into a public thread")
		}
	})

	t.Run("an unknown thread is refused", func(t *testing.T) {
		if s.isThreadOwnerForwarding(ownerActor, "https://"+domain+"/federation/users/alice/posts/nope") {
			t.Error("allowed a forward to introduce a thread this instance never had")
		}
	})
}
