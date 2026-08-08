package federation

import (
	"encoding/json"
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

// TestResolveFederatableTargetForSeparatesSignerFromActor covers AGORA-341's
// split between who signed an interaction and whose relationship authorises it.
// They are the same for a direct Like and differ for a forwarded one, and
// conflating them either records the thread owner as having reacted to their
// own post or refuses a legitimate forward.
func TestResolveFederatableTargetForSeparatesSignerFromActor(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano() % 1_000_000

	var authorID string
	if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`,
		fmt.Sprintf("a341%d", unique)).Scan(&authorID); err != nil {
		t.Fatalf("insert author: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })
	t.Cleanup(func() { db.Exec(`DELETE FROM friendships WHERE requester_id = $1 OR addressee_id = $1`, authorID) })

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility) VALUES ($1,'hi','friends') RETURNING id
	`, authorID).Scan(&postID); err != nil {
		t.Fatalf("insert post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })
	// localPostIDFromURL only resolves the actor-scoped form the actor document
	// itself emits: <instance>/federation/users/<name>/posts/<id>.
	postURL := fmt.Sprintf("https://local.example/federation/users/a341%d/posts/%s", unique, postID)

	domain := fmt.Sprintf("p341-%d.example", unique)
	friendActor := "https://" + domain + "/federation/users/bob"
	var friendID string
	db.QueryRow(`INSERT INTO users (username,email,password_hash,is_remote,remote_instance,ap_actor_url)
	             VALUES ($1,$1,'x',true,$2,$3) RETURNING id`,
		fmt.Sprintf("f341%d", unique), domain, friendActor).Scan(&friendID)
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, friendID) })
	db.Exec(`INSERT INTO friendships (requester_id,addressee_id,status) VALUES ($1,$2,'accepted')`, authorID, friendID)

	strangerActor := "https://" + domain + "/federation/users/mallory"

	t.Run("a friend's own reaction is authorised", func(t *testing.T) {
		if _, _, ok := s.resolveFederatableTargetFor(friendActor, friendActor, postURL); !ok {
			t.Error("refused an accepted friend reacting to a friends-only post")
		}
	})

	t.Run("a stranger's own reaction is refused", func(t *testing.T) {
		if _, _, ok := s.resolveFederatableTargetFor(strangerActor, strangerActor, postURL); ok {
			t.Error("allowed a stranger to react to a friends-only post")
		}
	})

	t.Run("a forward is authorised without any relationship to the liker", func(t *testing.T) {
		// authorizeAs empty means the caller already verified thread ownership.
		// The liker being unknown to us is expected and must not matter.
		if _, _, ok := s.resolveFederatableTargetFor(strangerActor, "", postURL); !ok {
			t.Error("refused a forwarded reaction, so counts stay inconsistent across the thread")
		}
	})
}

// ── AGORA-342: friend-list audiences ──────────────────────────────────────────

// TestListPostVisibilityIsFailClosed pins the storage decision. A friend-list
// post lands as 'private' because every existing feed filter already excludes
// that value, so a query site overlooked during this change hides the post
// rather than publishing it. Were this to drift to a value the filters admit,
// somebody's limited post becomes visible to their whole instance.
func TestListPostVisibilityIsFailClosed(t *testing.T) {
	if got := friendsOnlyVisibility("list", false); got != "private" {
		t.Errorf("friendsOnlyVisibility(\"list\", false) = %q, want \"private\"", got)
	}
	// Same contradiction rule as friends-only: claiming a limited audience while
	// addressing Public has already published it.
	if got := friendsOnlyVisibility("list", true); got != "public" {
		t.Errorf("friendsOnlyVisibility(\"list\", true) = %q, want \"public\"", got)
	}
}

// TestRemoteListRecipients covers who a friend-list post is addressed to: the
// members of that one list who live elsewhere. A member of a different list must
// not be swept in, which would deliver the post to somebody the author did not
// choose.
func TestRemoteListRecipients(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()

	var localID string
	localName := fmt.Sprintf("fl342_own_%d", unique)
	if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`, localName).Scan(&localID); err != nil {
		t.Fatalf("seeding the list owner failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, localID) })

	peerDomain := fmt.Sprintf("flpeer342-%d.example", unique)
	mk := func(suffix, name string) string {
		var id string
		uname := fmt.Sprintf("fl342_%s_%d", suffix, unique)
		if err := db.QueryRow(`
			INSERT INTO users (username,email,password_hash,is_remote,remote_instance,ap_actor_url)
			VALUES ($1,$1,'x',true,$2,$3) RETURNING id
		`, uname, peerDomain, "https://"+peerDomain+"/federation/users/"+name).Scan(&id); err != nil {
			t.Fatalf("seeding %s failed: %v", suffix, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	inList := mk("inlist", "bob")
	otherList := mk("otherlist", "carol")

	mkGroup := func(name string, members ...string) string {
		var gid string
		if err := db.QueryRow(`INSERT INTO friend_groups (user_id,name) VALUES ($1,$2) RETURNING id`, localID, name).Scan(&gid); err != nil {
			t.Fatalf("seeding list %q failed: %v", name, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM friend_groups WHERE id = $1`, gid) })
		for _, m := range members {
			if _, err := db.Exec(`INSERT INTO friend_group_members (group_id,friend_id) VALUES ($1,$2)`, gid, m); err != nil {
				t.Fatalf("seeding membership failed: %v", err)
			}
		}
		return gid
	}
	closeFriends := mkGroup(fmt.Sprintf("Close Friends %d", unique), inList)
	mkGroup(fmt.Sprintf("Work %d", unique), otherList)

	got := s.remoteListRecipients(localID, closeFriends)

	if len(got) != 1 {
		t.Fatalf("got %d recipients, want 1 (only the member of that list)", len(got))
	}
	if want := "https://" + peerDomain + "/federation/users/bob"; got[0].actorURL != want {
		t.Errorf("actorURL = %q, want %q; a member of another list must never be addressed", got[0].actorURL, want)
	}
}

// TestRecordPostAudienceIgnoresUnknownActors covers the receiving end. Being
// named in the addressing is the whole of the permission, so recording a user
// who was not named would hand them somebody else's limited post.
func TestRecordPostAudienceIgnoresUnknownActors(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()

	mkLocal := func(suffix string) (string, string) {
		var id string
		uname := fmt.Sprintf("fl342r_%s_%d", suffix, unique)
		if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`, uname).Scan(&id); err != nil {
			t.Fatalf("seeding %s failed: %v", suffix, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id, uname
	}
	addressedID, addressedName := mkLocal("addressed")
	bystanderID, _ := mkLocal("bystander")

	var authorID string
	if err := db.QueryRow(`
		INSERT INTO users (username,email,password_hash,is_remote,remote_instance,ap_actor_url)
		VALUES ($1,$1,'x',true,'peer342.example','https://peer342.example/federation/users/alice') RETURNING id
	`, fmt.Sprintf("fl342r_author_%d", unique)).Scan(&authorID); err != nil {
		t.Fatalf("seeding the remote author failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id,content,visibility) VALUES ($1,'limited','private') RETURNING id
	`, authorID).Scan(&postID); err != nil {
		t.Fatalf("seeding the post failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	n := s.recordPostAudience(postID, []string{
		"https://local.example/federation/users/" + addressedName,
		"https://peer342.example/federation/users/nobody-we-host",
		"https://www.w3.org/ns/activitystreams#Public",
	})
	if n != 1 {
		t.Fatalf("recorded %d local recipients, want 1", n)
	}

	var addressedHas, bystanderHas bool
	db.QueryRow(`SELECT EXISTS(SELECT 1 FROM post_audience WHERE post_id=$1 AND user_id=$2)`, postID, addressedID).Scan(&addressedHas)
	db.QueryRow(`SELECT EXISTS(SELECT 1 FROM post_audience WHERE post_id=$1 AND user_id=$2)`, postID, bystanderID).Scan(&bystanderHas)
	if !addressedHas {
		t.Error("the addressed user has no audience row, so the post they were sent stays invisible to them")
	}
	if bystanderHas {
		t.Error("a user who was never addressed got an audience row, which hands them somebody else's limited post")
	}
}

// TestIsAddressedListMember covers the authorisation counterpart: exactly the
// people a list post was delivered to may reply to it or react to it.
func TestIsAddressedListMember(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()

	var authorID string
	if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`,
		fmt.Sprintf("fl342m_author_%d", unique)).Scan(&authorID); err != nil {
		t.Fatalf("seeding the author failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	peerDomain := fmt.Sprintf("flmpeer342-%d.example", unique)
	memberActor := "https://" + peerDomain + "/federation/users/bob"
	strangerActor := "https://" + peerDomain + "/federation/users/mallory"

	var memberID string
	if err := db.QueryRow(`
		INSERT INTO users (username,email,password_hash,is_remote,remote_instance,ap_actor_url)
		VALUES ($1,$1,'x',true,$2,$3) RETURNING id
	`, fmt.Sprintf("fl342m_member_%d", unique), peerDomain, memberActor).Scan(&memberID); err != nil {
		t.Fatalf("seeding the member failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, memberID) })

	var groupID string
	if err := db.QueryRow(`INSERT INTO friend_groups (user_id,name) VALUES ($1,$2) RETURNING id`,
		authorID, fmt.Sprintf("Close Friends %d", unique)).Scan(&groupID); err != nil {
		t.Fatalf("seeding the list failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM friend_groups WHERE id = $1`, groupID) })
	if _, err := db.Exec(`INSERT INTO friend_group_members (group_id,friend_id) VALUES ($1,$2)`, groupID, memberID); err != nil {
		t.Fatalf("seeding membership failed: %v", err)
	}

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id,content,visibility,group_id) VALUES ($1,'limited','group',$2) RETURNING id
	`, authorID, groupID).Scan(&postID); err != nil {
		t.Fatalf("seeding the post failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	if !s.isAddressedListMember(postID, memberActor) {
		t.Error("a member of the list was refused, so their reply to a post they can see would vanish")
	}
	if s.isAddressedListMember(postID, strangerActor) {
		t.Error("a non-member was admitted into a limited thread")
	}
	if s.isAddressedListMember(postID, "") {
		t.Error("an empty actor was admitted")
	}
}

// ── AGORA-343: edits and deletes of a limited post ────────────────────────────

// TestAddressLimitedRewritesBothLevels pins the part that decides whether an
// edit of a limited post stays limited on arrival.
//
// The activity and its object both have to be rewritten. A receiver decides
// what a post is from the object's own to/cc (friendsOnlyVisibility does
// exactly that), so an activity addressed privately around an object still
// claiming Public would be stored public by the very people it was limited to.
func TestAddressLimitedRewritesBothLevels(t *testing.T) {
	const public = "https://www.w3.org/ns/activitystreams#Public"

	activity := map[string]any{
		"type": "Update",
		"to":   []string{public},
		"object": map[string]any{
			"type": "Note",
			"to":   []string{public},
			"cc":   []string{"https://peer.example/users/alice/followers"},
		},
	}
	recipients := []friendRecipient{
		{actorURL: "https://peer.example/federation/users/bob", inboxURL: "https://peer.example/federation/inbox"},
		{actorURL: "https://peer.example/federation/users/carol", inboxURL: "https://peer.example/federation/inbox"},
	}

	addressLimited(activity, recipients, "list")

	obj, ok := activity["object"].(map[string]any)
	if !ok {
		t.Fatal("the object went missing")
	}
	for name, level := range map[string]map[string]any{"activity": activity, "object": obj} {
		to, _ := level["to"].([]string)
		if len(to) != 2 {
			t.Errorf("%s: to has %d entries, want 2", name, len(to))
		}
		if isAddressedPublicly(to) {
			t.Errorf("%s: still addressed to Public, so a receiver stores this post public", name)
		}
		if cc, _ := level["cc"].([]string); len(cc) != 0 {
			t.Errorf("%s: cc = %v, want empty; the followers collection is not this post's audience", name, cc)
		}
		if level[audienceMarker] != "list" {
			t.Errorf("%s: audience marker = %v, want \"list\"", name, level[audienceMarker])
		}
	}

	// The round trip that matters: what a receiver would decide from this.
	to, _ := obj["to"].([]string)
	cc, _ := obj["cc"].([]string)
	if got := friendsOnlyVisibility("list", isAddressedPublicly(to, cc)); got != "private" {
		t.Errorf("a receiver would store this as %q, want \"private\" (AGORA-342's fail-closed value)", got)
	}
}

// TestDeliverLimitedDeduplicatesByInbox covers the per-instance addressing.
// Agora serves one shared inbox per instance, so several recipients on the same
// peer must not each get their own copy of an identical activity.
func TestDeliverLimitedDeduplicatesByInbox(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()

	var userID string
	if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`,
		fmt.Sprintf("fl343_%d", unique)).Scan(&userID); err != nil {
		t.Fatalf("seeding the author failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })
	t.Cleanup(func() { db.Exec(`DELETE FROM ap_delivery_queue WHERE actor_user_id = $1`, userID) })

	peerInbox := fmt.Sprintf("https://peer343-%d.example/federation/inbox", unique)
	other := fmt.Sprintf("https://other343-%d.example/federation/inbox", unique)
	recipients := []friendRecipient{
		{actorURL: "https://a.example/users/bob", inboxURL: peerInbox},
		{actorURL: "https://a.example/users/carol", inboxURL: peerInbox},
		{actorURL: "https://b.example/users/dave", inboxURL: other},
		{actorURL: "https://b.example/users/erin", inboxURL: ""}, // no inbox: skipped
	}

	if n := s.deliverLimited(userID, recipients, map[string]any{"type": "Delete"}); n != 2 {
		t.Errorf("delivered to %d inboxes, want 2 (one per instance, skipping the recipient with no inbox)", n)
	}

	var queued int
	db.QueryRow(`SELECT COUNT(*) FROM ap_delivery_queue WHERE actor_user_id = $1`, userID).Scan(&queued)
	if queued != 2 {
		t.Errorf("queued %d deliveries, want 2", queued)
	}
}

// TestLimitedPostAudienceSurvivesDelete is the one that would have caught the
// obvious way to write this wrong. DeletePost soft-deletes the row before it
// broadcasts, so an audience query filtering on deleted_at returns nobody for
// every delete, and every recipient silently keeps their copy.
func TestLimitedPostAudienceSurvivesDelete(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()

	var authorID string
	if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`,
		fmt.Sprintf("fl343d_a_%d", unique)).Scan(&authorID); err != nil {
		t.Fatalf("seeding the author failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	peerDomain := fmt.Sprintf("peer343d-%d.example", unique)
	var friendID string
	if err := db.QueryRow(`
		INSERT INTO users (username,email,password_hash,is_remote,remote_instance,ap_actor_url)
		VALUES ($1,$1,'x',true,$2,$3) RETURNING id
	`, fmt.Sprintf("fl343d_f_%d", unique), peerDomain, "https://"+peerDomain+"/federation/users/bob").Scan(&friendID); err != nil {
		t.Fatalf("seeding the friend failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, friendID) })
	if _, err := db.Exec(`INSERT INTO friendships (requester_id,addressee_id,status) VALUES ($1,$2,'accepted')`, authorID, friendID); err != nil {
		t.Fatalf("seeding the friendship failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM friendships WHERE requester_id = $1`, authorID) })

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id,content,visibility) VALUES ($1,'hi','friends') RETURNING id
	`, authorID).Scan(&postID); err != nil {
		t.Fatalf("seeding the post failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	recipients, limited := s.limitedPostAudience(authorID, postID)
	if !limited || len(recipients) != 1 {
		t.Fatalf("before delete: limited=%v, %d recipients; want true, 1", limited, len(recipients))
	}

	// Exactly what DeletePost does before it broadcasts.
	db.Exec(`UPDATE posts SET deleted_at = NOW() WHERE id = $1`, postID)

	recipients, limited = s.limitedPostAudience(authorID, postID)
	if !limited {
		t.Error("a deleted post no longer reads as limited, so its delete would go to followers and Public")
	}
	if len(recipients) != 1 {
		t.Errorf("after delete: %d recipients, want 1. Nobody is told to remove their copy.", len(recipients))
	}
}

// TestLimitedPostAudienceClassification covers the branch that decides whether a
// post takes the limited path at all. A public post must keep the follower and
// relay delivery it has always had.
func TestLimitedPostAudienceClassification(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()

	var authorID string
	if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`,
		fmt.Sprintf("fl343c_%d", unique)).Scan(&authorID); err != nil {
		t.Fatalf("seeding the author failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	mkPost := func(visibility string) string {
		var id string
		if err := db.QueryRow(`INSERT INTO posts (author_id,content,visibility) VALUES ($1,'x',$2) RETURNING id`,
			authorID, visibility).Scan(&id); err != nil {
			t.Fatalf("seeding a %s post failed: %v", visibility, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, id) })
		return id
	}

	if _, limited := s.limitedPostAudience(authorID, mkPost("public")); limited {
		t.Error("a public post took the limited path, losing its follower and relay delivery")
	}
	if _, limited := s.limitedPostAudience(authorID, mkPost("friends")); !limited {
		t.Error("a friends-only post took the public path, broadcasting it to followers and Public")
	}
	// 'private' has no federated audience, but must still not fall through to
	// the public branch.
	if _, limited := s.limitedPostAudience(authorID, mkPost("private")); !limited {
		t.Error("a private post took the public path")
	}
	if _, limited := s.limitedPostAudience(authorID, "00000000-0000-0000-0000-000000000000"); limited {
		t.Error("an unresolvable post reported as limited")
	}
}

// ── AGORA-344: withdrawal fan-out ─────────────────────────────────────────────

// TestUndoLikeFanOutIdentity covers the one thing Undo needed that Like already
// had: telling the signer apart from the person who acted.
//
// For a forwarded withdrawal those differ, and keying the removal on the signer
// would try to un-react on behalf of the thread's owner. The reaction that was
// meant to go stays on every instance downstream, which is the exact drift this
// ticket exists to stop.
func TestUndoLikeFanOutIdentity(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()

	var authorID string
	if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`,
		fmt.Sprintf("fl344_a_%d", unique)).Scan(&authorID); err != nil {
		t.Fatalf("seeding the author failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	peerDomain := fmt.Sprintf("peer344-%d.example", unique)
	likerActor := "https://" + peerDomain + "/federation/users/bob"
	var likerID string
	if err := db.QueryRow(`
		INSERT INTO users (username,email,password_hash,is_remote,remote_instance,ap_actor_url)
		VALUES ($1,$1,'x',true,$2,$3) RETURNING id
	`, fmt.Sprintf("fl344_b_%d", unique), peerDomain, likerActor).Scan(&likerID); err != nil {
		t.Fatalf("seeding the liker failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, likerID) })
	if _, err := db.Exec(`INSERT INTO friendships (requester_id,addressee_id,status) VALUES ($1,$2,'accepted')`, authorID, likerID); err != nil {
		t.Fatalf("seeding the friendship failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM friendships WHERE requester_id = $1`, authorID) })

	var username string
	db.QueryRow(`SELECT username FROM users WHERE id = $1`, authorID).Scan(&username)

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id,content,visibility) VALUES ($1,'hi','friends') RETURNING id
	`, authorID).Scan(&postID); err != nil {
		t.Fatalf("seeding the post failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })
	postURL := "https://local.example/federation/users/" + username + "/posts/" + postID

	seedLike := func() {
		if _, err := db.Exec(`INSERT INTO reactions (user_id,post_id,reaction_type) VALUES ($1,$2,'like')
			ON CONFLICT (user_id,post_id) DO UPDATE SET reaction_type = 'like'`, likerID, postID); err != nil {
			t.Fatalf("seeding the reaction failed: %v", err)
		}
	}
	stillLiked := func() bool {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM reactions WHERE user_id=$1 AND post_id=$2`, likerID, postID).Scan(&n)
		return n > 0
	}
	objectRaw, _ := json.Marshal(postURL)

	t.Run("a direct withdrawal removes the reaction", func(t *testing.T) {
		seedLike()
		s.handleInboundUndoLike(likerActor, likerActor, objectRaw, nil)
		if stillLiked() {
			t.Error("the reaction survived its own author's withdrawal")
		}
	})

	t.Run("a stranger cannot withdraw somebody else's reaction", func(t *testing.T) {
		seedLike()
		t.Cleanup(func() { db.Exec(`DELETE FROM reactions WHERE user_id = $1`, likerID) })
		// A signer who does not own the thread, claiming to relay the liker's
		// withdrawal. isThreadOwnerForwarding must refuse it.
		s.handleInboundUndoLike("https://"+peerDomain+"/federation/users/mallory", likerActor, objectRaw, []byte(`{"type":"Undo"}`))
		if !stillLiked() {
			t.Error("a peer that does not own this thread cleared somebody else's reaction")
		}
	})
}
