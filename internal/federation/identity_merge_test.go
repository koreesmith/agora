package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
)

// AGORA-346: the same person held as two rows, merged onto their ActivityPub
// identity.
//
// The stakes decide how this is tested. posts.author_id is ON DELETE CASCADE,
// so a merge that removes the losing row before moving everything off it
// destroys somebody's posts. The assertions below are therefore about content
// surviving, not about the row count.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestMergeDuplicateIdentity(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()
	peer := fmt.Sprintf("peer346-%d.example", unique)
	actorURL := "https://" + peer + "/federation/users/bob"

	var localA, localB string
	for i, dst := range []*string{&localA, &localB} {
		if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`,
			fmt.Sprintf("m346_l%d_%d", i, unique)).Scan(dst); err != nil {
			t.Fatalf("seeding local user %d failed: %v", i, err)
		}
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, localA, localB) })

	// The legacy stub: keyed on (remote_user_id, remote_instance), no actor URL.
	var loser string
	if err := db.QueryRow(`
		INSERT INTO users (username,email,password_hash,display_name,is_remote,remote_instance,remote_user_id)
		VALUES ($1,$1,'x','Bob (legacy)',true,$2,'bob') RETURNING id
	`, fmt.Sprintf("m346_legacy_%d", unique), peer).Scan(&loser); err != nil {
		t.Fatalf("seeding the legacy stub failed: %v", err)
	}
	// The ActivityPub row for the same human.
	var winner string
	if err := db.QueryRow(`
		INSERT INTO users (username,email,password_hash,display_name,is_remote,remote_instance,ap_actor_url)
		VALUES ($1,$1,'x','Bob',true,$2,$3) RETURNING id
	`, fmt.Sprintf("m346_ap_%d", unique), peer, actorURL).Scan(&winner); err != nil {
		t.Fatalf("seeding the ActivityPub row failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, loser, winner) })

	// Content split across the two rows, which is the whole problem.
	var legacyPost string
	if err := db.QueryRow(`INSERT INTO posts (author_id,content,visibility) VALUES ($1,'from the legacy row','public') RETURNING id`, loser).Scan(&legacyPost); err != nil {
		t.Fatalf("seeding the legacy post failed: %v", err)
	}
	var apPost string
	if err := db.QueryRow(`INSERT INTO posts (author_id,content,visibility) VALUES ($1,'from the AP row','public') RETURNING id`, winner).Scan(&apPost); err != nil {
		t.Fatalf("seeding the AP post failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id IN ($1,$2)`, legacyPost, apPost) })

	// A friendship only the legacy row has, which must move.
	if _, err := db.Exec(`INSERT INTO friendships (requester_id,addressee_id,status) VALUES ($1,$2,'accepted')`, localA, loser); err != nil {
		t.Fatalf("seeding the movable friendship failed: %v", err)
	}
	// A friendship BOTH rows have with the same local user. The unique
	// constraint on the ordered pair means this one cannot move, and resolving
	// it precisely rather than dropping the whole table's worth is the reason
	// the merge works a row at a time.
	if _, err := db.Exec(`INSERT INTO friendships (requester_id,addressee_id,status) VALUES ($1,$2,'accepted'),($1,$3,'accepted')`, localB, loser, winner); err != nil {
		t.Fatalf("seeding the colliding friendship failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM friendships WHERE requester_id IN ($1,$2)`, localA, localB) })

	var outcome string
	if err := db.QueryRow(`SELECT agora_merge_duplicate_identity($1,$2)`, loser, winner).Scan(&outcome); err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if outcome != "merged" {
		t.Errorf("outcome = %q, want \"merged\"", outcome)
	}

	// Content first. Nothing may be destroyed by the merge.
	for _, c := range []struct{ id, what string }{
		{legacyPost, "the post written under the legacy identity"},
		{apPost, "the post written under the ActivityPub identity"},
	} {
		var author string
		if err := db.QueryRow(`SELECT author_id FROM posts WHERE id = $1 AND deleted_at IS NULL`, c.id).Scan(&author); err != nil {
			t.Fatalf("%s is gone: %v. posts.author_id is ON DELETE CASCADE, so this is what a premature delete destroys.", c.what, err)
		}
		if author != winner {
			t.Errorf("%s hangs off %s, want the surviving row %s", c.what, author, winner)
		}
	}

	// The movable friendship moved; the colliding one resolved without
	// disturbing the winner's own.
	var movedFriendship, keptFriendship bool
	db.QueryRow(`SELECT EXISTS(SELECT 1 FROM friendships WHERE requester_id=$1 AND addressee_id=$2)`, localA, winner).Scan(&movedFriendship)
	db.QueryRow(`SELECT EXISTS(SELECT 1 FROM friendships WHERE requester_id=$1 AND addressee_id=$2)`, localB, winner).Scan(&keptFriendship)
	if !movedFriendship {
		t.Error("the friendship held only by the legacy row did not move, so that person lost a friend")
	}
	if !keptFriendship {
		t.Error("the winner's own friendship was destroyed while resolving the collision")
	}

	// The legacy key rides along, so the historical identity still resolves.
	var survivingRemoteID, survivingActor string
	if err := db.QueryRow(`SELECT COALESCE(remote_user_id,''), COALESCE(ap_actor_url,'') FROM users WHERE id = $1`, winner).
		Scan(&survivingRemoteID, &survivingActor); err != nil {
		t.Fatalf("the surviving row is gone: %v", err)
	}
	if survivingRemoteID != "bob" {
		t.Errorf("remote_user_id = %q, want \"bob\"; remoteUserIDForActor's legacy lookup stops working without it", survivingRemoteID)
	}
	if survivingActor != actorURL {
		t.Errorf("ap_actor_url = %q, want %q", survivingActor, actorURL)
	}

	var loserGone bool
	db.QueryRow(`SELECT NOT EXISTS(SELECT 1 FROM users WHERE id = $1)`, loser).Scan(&loserGone)
	if !loserGone {
		t.Error("the duplicate row survived, so one person is still two rows")
	}

	// One identity, resolvable by either key.
	if got := s.remoteUserIDForActor(actorURL); got != winner {
		t.Errorf("remoteUserIDForActor = %q, want %q", got, winner)
	}
}

// TestMergeDuplicateIdentityRefusesNonPairs covers the guards. Merging a row
// into itself, or into nothing, must do nothing at all rather than delete.
func TestMergeDuplicateIdentityRefusesNonPairs(t *testing.T) {
	db := testFriendshipService(t)
	unique := time.Now().UnixNano()

	var id string
	if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`,
		fmt.Sprintf("m346_self_%d", unique)).Scan(&id); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })

	var outcome string
	if err := db.QueryRow(`SELECT agora_merge_duplicate_identity($1,$1)`, id).Scan(&outcome); err != nil {
		t.Fatalf("merge errored: %v", err)
	}
	if outcome == "merged" {
		t.Error("a row was merged into itself")
	}
	var stillThere bool
	db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, id).Scan(&stillThere)
	if !stillThere {
		t.Error("merging a row into itself deleted it")
	}
}
