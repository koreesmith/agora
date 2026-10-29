package atproto

import (
	"fmt"
	"testing"
	"time"

)

// AGORA-299: a reply's `root` ref decides which conversation it appears in on
// Bluesky, and the two possible sources disagree for exactly the posts that
// matter.
//
// Every inbound Bluesky post is stored flat, so the local parent walk always
// concludes an ingested post is its own root. That is right for a post that
// genuinely is one and wrong for a post that was itself a reply, which is how
// quote targets and most ingested content arrive. The stored ref came off the
// parent's own record and settles it.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestBlueskyThreadRoot(t *testing.T) {
	db := testDB(t)
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := &Service{db: db}
	unique := time.Now().UnixNano()

	var authorID string
	if err := db.QueryRow(`
		INSERT INTO users (username,email,password_hash,is_remote,remote_instance,atproto_did)
		VALUES ($1,$1,'x',true,'bsky.app',$2) RETURNING id
	`, fmt.Sprintf("bsky299_%d", unique), fmt.Sprintf("did:plc:x%d", unique)).Scan(&authorID); err != nil {
		t.Fatalf("seeding the author failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	mkPost := func(tag, rootURI, rootCID string) string {
		var id string
		if err := db.QueryRow(`
			INSERT INTO posts (author_id, content, visibility, is_remote, remote_instance,
			                   remote_post_id, remote_post_cid, bsky_root_uri, bsky_root_cid)
			VALUES ($1,'x','public',true,'bsky.app',$2,$3,$4,$5) RETURNING id
		`, authorID,
			fmt.Sprintf("at://did:plc:x/app.bsky.feed.post/%s-%d", tag, unique),
			fmt.Sprintf("cid-%s-%d", tag, unique),
			rootURI, rootCID).Scan(&id); err != nil {
			t.Fatalf("seeding %s failed: %v", tag, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, id) })
		return id
	}

	t.Run("an ingested reply federates against the true thread root", func(t *testing.T) {
		trueRoot := fmt.Sprintf("at://did:plc:someone/app.bsky.feed.post/root-%d", unique)
		trueCID := fmt.Sprintf("cid-trueroot-%d", unique)
		// A post that is itself a reply, which is how a quote target arrives.
		parent := mkPost("wasareply", trueRoot, trueCID)

		uri, cid, ok := s.blueskyThreadRoot(parent)
		if !ok {
			t.Fatal("no root resolved, so the reply would not federate at all")
		}
		if uri != trueRoot || cid != trueCID {
			t.Errorf("root = %q/%q, want the true thread root %q/%q.\nThe local walk would have named the ingested post here, and Bluesky threads on root, so the reply lands in a detached mini-thread.", uri, cid, trueRoot, trueCID)
		}
	})

	t.Run("a genuinely top-level post is its own root", func(t *testing.T) {
		// No stored ref, which is meaningful rather than missing: the record
		// carried no reply.root, so this really is a thread root.
		top := mkPost("toplevel", "", "")

		uri, cid, ok := s.blueskyThreadRoot(top)
		if !ok {
			t.Fatal("no root resolved for a top-level post")
		}
		var wantURI, wantCID string
		db.QueryRow(`SELECT remote_post_id, remote_post_cid FROM posts WHERE id = $1`, top).Scan(&wantURI, &wantCID)
		if uri != wantURI || cid != wantCID {
			t.Errorf("root = %q/%q, want the post itself %q/%q", uri, cid, wantURI, wantCID)
		}
	})

	t.Run("a half-stored ref is not trusted", func(t *testing.T) {
		// A uri with no cid cannot form a strong ref, so it must fall back
		// rather than emit a malformed one.
		half := mkPost("halfref", fmt.Sprintf("at://did:plc:someone/app.bsky.feed.post/half-%d", unique), "")

		uri, _, ok := s.blueskyThreadRoot(half)
		if !ok {
			t.Fatal("no root resolved")
		}
		var self string
		db.QueryRow(`SELECT remote_post_id FROM posts WHERE id = $1`, half).Scan(&self)
		if uri != self {
			t.Errorf("root = %q, want the fallback %q; a ref with no cid is not usable", uri, self)
		}
	})
}
