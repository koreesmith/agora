package hidden

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-309: hiding is a client of one. The two properties that matter are that
// it works for the person who hid the post, and that it is invisible to
// everyone else including the author. A filter applied too broadly would be a
// moderation action nobody asked for.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestHiddenPostsAreScopedToOneViewer(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	unique := time.Now().UnixNano()

	mkUser := func(tag string) string {
		var id string
		if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`,
			fmt.Sprintf("h309_%s_%d", tag, unique)).Scan(&id); err != nil {
			t.Fatalf("seeding %s failed: %v", tag, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	author, hider, bystander := mkUser("author"), mkUser("hider"), mkUser("bystander")

	var postID string
	if err := db.QueryRow(`INSERT INTO posts (author_id,content,visibility) VALUES ($1,'x','public') RETURNING id`, author).Scan(&postID); err != nil {
		t.Fatalf("seeding the post failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	// The clause every feed query uses, asserted directly rather than through a
	// handler, so this covers what the queries actually run.
	visibleTo := func(viewer string) bool {
		var visible bool
		db.QueryRow(`
			SELECT NOT EXISTS(SELECT 1 FROM hidden_posts WHERE user_id = $1 AND post_id = $2)
		`, viewer, postID).Scan(&visible)
		return visible
	}

	if _, err := db.Exec(`INSERT INTO hidden_posts (user_id, post_id) VALUES ($1,$2)`, hider, postID); err != nil {
		t.Fatalf("hiding failed: %v", err)
	}

	if visibleTo(hider) {
		t.Error("the post is still visible to the person who hid it")
	}
	if !visibleTo(bystander) {
		t.Error("hiding a post removed it for somebody else, which makes a personal preference into a moderation action")
	}
	if !visibleTo(author) {
		t.Error("hiding a post removed it for its author")
	}

	t.Run("hiding twice is not an error", func(t *testing.T) {
		if _, err := db.Exec(`INSERT INTO hidden_posts (user_id, post_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, hider, postID); err != nil {
			t.Errorf("a repeated hide errored: %v", err)
		}
	})

	t.Run("deleting the post leaves no orphan row", func(t *testing.T) {
		var other string
		db.QueryRow(`INSERT INTO posts (author_id,content,visibility) VALUES ($1,'y','public') RETURNING id`, author).Scan(&other)
		db.Exec(`INSERT INTO hidden_posts (user_id, post_id) VALUES ($1,$2)`, hider, other)
		db.Exec(`DELETE FROM posts WHERE id = $1`, other)

		var n int
		db.QueryRow(`SELECT COUNT(*) FROM hidden_posts WHERE post_id = $1`, other).Scan(&n)
		if n != 0 {
			t.Errorf("%d orphaned hidden_posts row(s) survived the post", n)
		}
	})
}
