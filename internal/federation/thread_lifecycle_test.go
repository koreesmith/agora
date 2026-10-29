package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
)

// AGORA-347: an edit or a delete of a reply has to reach everyone the reply
// reached, which for a limited thread means the thread's owner forwards it on.
//
// Two things have to hold for that to work, and each fails silently on its own:
// the origin has to forward, and the receiver has to accept a forward whose
// attributedTo is not its signer. The second is the one that would make the
// first look implemented while doing nothing.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestThreadOwnerForwardingComment(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()
	peer := fmt.Sprintf("peer347-%d.example", unique)
	ownerActor := "https://" + peer + "/federation/users/alice"
	replierActor := "https://" + peer + "/federation/users/bob"

	mkRemote := func(tag, instance, actorURL string) string {
		var id string
		if err := db.QueryRow(`
			INSERT INTO users (username,email,password_hash,is_remote,remote_instance,ap_actor_url)
			VALUES ($1,$1,'x',true,$2,$3) RETURNING id
		`, fmt.Sprintf("t347_%s_%d", tag, unique), instance, actorURL).Scan(&id); err != nil {
			t.Fatalf("seeding %s failed: %v", tag, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	ownerID := mkRemote("owner", peer, ownerActor)
	replierID := mkRemote("replier", peer, replierActor)

	// Each thread needs its own remote ids: two subtests use the same
	// visibility, and remote_post_id is unique.
	seq := 0
	mkThread := func(visibility string, withAudience bool) (rootID, commentID string) {
		seq++
		tag := fmt.Sprintf("%s-%d-%d", visibility, seq, unique)
		if err := db.QueryRow(`
			INSERT INTO posts (author_id, content, visibility, remote_post_id, remote_instance, is_remote)
			VALUES ($1, 'root', $2, $3, $4, true) RETURNING id
		`, ownerID, visibility, fmt.Sprintf("https://%s/p/%s", peer, tag), peer).Scan(&rootID); err != nil {
			t.Fatalf("seeding the %s root failed: %v", visibility, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, rootID) })

		if withAudience {
			var localID string
			db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`,
				fmt.Sprintf("t347_aud_%s", tag)).Scan(&localID)
			t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, localID) })
			db.Exec(`INSERT INTO post_audience (post_id, user_id) VALUES ($1,$2)`, rootID, localID)
		}

		if err := db.QueryRow(`
			INSERT INTO posts (author_id, content, visibility, parent_id, remote_post_id, remote_instance, is_remote)
			VALUES ($1, 'reply', $2, $3, $4, $5, true) RETURNING id
		`, replierID, visibility, rootID, fmt.Sprintf("https://%s/c/%s", peer, tag), peer).Scan(&commentID); err != nil {
			t.Fatalf("seeding the %s comment failed: %v", visibility, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, commentID) })
		return rootID, commentID
	}

	t.Run("the thread owner may forward into a friends-only thread", func(t *testing.T) {
		_, commentID := mkThread("friends", false)
		if !s.isThreadOwnerForwardingComment(ownerActor, commentID) {
			t.Error("the thread's owner was refused, so every forwarded edit and delete in this thread is discarded on arrival")
		}
	})

	t.Run("a received list thread counts, proven by its audience record", func(t *testing.T) {
		_, commentID := mkThread("private", true)
		if !s.isThreadOwnerForwardingComment(ownerActor, commentID) {
			t.Error("a friend-list thread was refused; AGORA-342 stores those 'private' and the audience row is what identifies them")
		}
	})

	t.Run("a private thread with no audience record is not one", func(t *testing.T) {
		_, commentID := mkThread("private", false)
		if s.isThreadOwnerForwardingComment(ownerActor, commentID) {
			t.Error("a genuinely private post was treated as a limited thread")
		}
	})

	t.Run("a public thread gains no bypass", func(t *testing.T) {
		_, commentID := mkThread("public", false)
		if s.isThreadOwnerForwardingComment(ownerActor, commentID) {
			t.Error("a public thread granted a forwarding bypass it has no use for")
		}
	})

	t.Run("nobody but the thread owner may forward", func(t *testing.T) {
		_, commentID := mkThread("friends", false)
		if s.isThreadOwnerForwardingComment(replierActor, commentID) {
			t.Error("the replier could forward into a thread they do not own, which lets a peer put words in somebody's mouth")
		}
		if s.isThreadOwnerForwardingComment("https://"+peer+"/federation/users/mallory", commentID) {
			t.Error("a stranger could forward into a thread they do not own")
		}
	})

	t.Run("a top-level post is not a forwardable comment", func(t *testing.T) {
		rootID, _ := mkThread("friends", false)
		if s.isThreadOwnerForwardingComment(ownerActor, rootID) {
			t.Error("a top-level post was treated as a comment; its author sends edits directly and needs no forward")
		}
	})
}

// TestRootPostIDOf covers the walk the fan-out depends on. Agora caps threads at
// root -> comment -> reply, so both depths must resolve to the same root.
func TestRootPostIDOf(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()

	var authorID string
	if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`,
		fmt.Sprintf("t347r_%d", unique)).Scan(&authorID); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	mk := func(parent *string) string {
		var id string
		if err := db.QueryRow(`INSERT INTO posts (author_id, content, visibility, parent_id) VALUES ($1,'x','friends',$2) RETURNING id`,
			authorID, parent).Scan(&id); err != nil {
			t.Fatalf("seeding a post failed: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, id) })
		return id
	}
	root := mk(nil)
	comment := mk(&root)
	reply := mk(&comment)

	if got := s.rootPostIDOf(root); got != root {
		t.Errorf("root resolved to %q, want itself", got)
	}
	if got := s.rootPostIDOf(comment); got != root {
		t.Errorf("comment resolved to %q, want the root %q", got, root)
	}
	if got := s.rootPostIDOf(reply); got != root {
		t.Errorf("reply-to-a-reply resolved to %q, want the root %q. Fan-out would address the wrong thread.", got, root)
	}
}

// TestForwardedDeleteIsApplied drives the real handler. The unit checks above
// prove the carve-out answers correctly; this proves the handler consults it,
// which is the difference between a fix and a function nobody calls.
func TestForwardedDeleteIsApplied(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()
	peer := fmt.Sprintf("fwd347-%d.example", unique)
	ownerActor := "https://" + peer + "/federation/users/alice"
	replierActor := "https://" + peer + "/federation/users/bob"

	mkRemote := func(tag, actorURL string) string {
		var id string
		if err := db.QueryRow(`
			INSERT INTO users (username,email,password_hash,is_remote,remote_instance,ap_actor_url)
			VALUES ($1,$1,'x',true,$2,$3) RETURNING id
		`, fmt.Sprintf("fwd347_%s_%d", tag, unique), peer, actorURL).Scan(&id); err != nil {
			t.Fatalf("seeding %s failed: %v", tag, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	ownerID := mkRemote("owner", ownerActor)
	replierID := mkRemote("replier", replierActor)

	var rootID string
	db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, remote_post_id, remote_instance, is_remote)
		VALUES ($1,'root','friends',$2,$3,true) RETURNING id
	`, ownerID, fmt.Sprintf("https://%s/p/%d", peer, unique), peer).Scan(&rootID)
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, rootID) })

	commentObjectID := fmt.Sprintf("https://%s/c/%d", peer, unique)
	var commentID string
	db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, parent_id, remote_post_id, remote_instance, is_remote)
		VALUES ($1,'reply','friends',$2,$3,$4,true) RETURNING id
	`, replierID, rootID, commentObjectID, peer).Scan(&commentID)
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, commentID) })

	deleted := func() bool {
		var gone bool
		db.QueryRow(`SELECT deleted_at IS NOT NULL FROM posts WHERE id = $1`, commentID).Scan(&gone)
		return gone
	}
	objectRaw := []byte(`"` + commentObjectID + `"`)

	t.Run("a stranger cannot delete the reply", func(t *testing.T) {
		s.handleInboundAPDelete("https://"+peer+"/federation/users/mallory", objectRaw, nil)
		if deleted() {
			t.Fatal("somebody who neither wrote the reply nor owns the thread deleted it")
		}
	})

	t.Run("the thread owner forwarding the replier's delete is applied", func(t *testing.T) {
		// Signed by Alice, but the reply is Bob's. Before AGORA-347 this was
		// refused outright, so the fan-out would have delivered into a wall.
		s.handleInboundAPDelete(ownerActor, objectRaw, []byte(`{"type":"Delete"}`))
		if !deleted() {
			t.Error("a forwarded delete was refused, so everyone but the thread author keeps a reply its author withdrew")
		}
	})
}
