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
