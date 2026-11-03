package atproto

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/agora-social/agora/internal/config"
)

func TestFlattenRepostForBluesky(t *testing.T) {
	link := "https://agora.example/post/abc"
	if got, want := flattenRepostForBluesky("", "Ada", "hello world", link),
		"🔁 Ada: hello world\n\n"+link; got != want {
		t.Errorf("bare repost:\n got:  %q\n want: %q", got, want)
	}
	if got, want := flattenRepostForBluesky("  so true  ", "Ada", "hello world", link),
		"so true\n\n🔁 Ada: hello world\n\n"+link; got != want {
		t.Errorf("with commentary:\n got:  %q\n want: %q", got, want)
	}
	if got, want := flattenRepostForBluesky("", "Ada", "", link), "🔁 Ada\n\n"+link; got != want {
		t.Errorf("image-only original:\n got:  %q\n want: %q", got, want)
	}

	long := flattenRepostForBluesky("", "Ada", strings.Repeat("word ", 200), link)
	if n := utf8.RuneCountInString(long); n > blueskyMaxGraphemes {
		t.Errorf("long repost is %d characters, over the %d limit", n, blueskyMaxGraphemes)
	}
	if !strings.HasSuffix(long, link) {
		t.Errorf("long repost lost its link back: %q", long)
	}
}

// AGORA-358. Requires the local agora-postgres-test instance; skips otherwise.
func TestDeliverAnnounceFlattensNonBlueskyRepost(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	var prevEnabled string
	db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'atproto_enabled'`).Scan(&prevEnabled)
	db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('atproto_enabled', 'true') ON CONFLICT (key) DO UPDATE SET value = 'true'`)
	t.Cleanup(func() {
		db.Exec(`UPDATE instance_settings SET value = $1 WHERE key = 'atproto_enabled'`, prevEnabled)
	})

	unique := time.Now().UnixNano()
	var baselineSeq int64
	db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM atproto_firehose_events`).Scan(&baselineSeq)
	t.Cleanup(func() { db.Exec(`DELETE FROM atproto_firehose_events WHERE seq > $1`, baselineSeq) })

	mkUser := func(tag string, atproto bool) string {
		name := fmt.Sprintf("agora358_%s_%d", tag, unique)
		var id string
		if err := db.QueryRow(`
			INSERT INTO users (username, email, password_hash, profile_private, atproto_enabled, atproto_did, display_name)
			VALUES ($1, $2, '', false, $3, $4, $5) RETURNING id
		`, name, name+"@example.invalid", atproto, "did:web:"+name, "Author "+tag).Scan(&id); err != nil {
			t.Fatalf("seed user %s: %v", tag, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	mkPost := func(author, content, visibility string, repostOf *string) string {
		var id string
		if err := db.QueryRow(`
			INSERT INTO posts (author_id, content, visibility, repost_of_id) VALUES ($1, $2, $3, $4) RETURNING id
		`, author, content, visibility, repostOf).Scan(&id); err != nil {
			t.Fatalf("seed post: %v", err)
		}
		t.Cleanup(func() {
			db.Exec(`DELETE FROM atproto_posts WHERE post_id = $1`, id)
			db.Exec(`DELETE FROM posts WHERE id = $1`, id)
		})
		return id
	}

	reposter, author := mkUser("reposter", true), mkUser("author", false)
	publicOrig := mkPost(author, "a local post with no Bluesky presence", "public", nil)
	friendsOrig := mkPost(author, "friends only", "friends", nil)
	listOrig := mkPost(author, "friend list post", "group", nil)

	s := NewService(db, &config.Config{InstanceDomain: "https://agora.example"}, nil)
	sent := func(repostID string) bool {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM atproto_posts WHERE post_id = $1`, repostID).Scan(&n)
		return n == 1
	}

	// Public repost of a public local post: goes out, and only once.
	repost := mkPost(reposter, "", "public", &publicOrig)
	s.DeliverAnnounce(reposter, repost, publicOrig)
	if !sent(repost) {
		t.Fatal("public repost of a non-Bluesky post was not federated")
	}
	var rkey string
	db.QueryRow(`SELECT rkey FROM atproto_posts WHERE post_id = $1`, repost).Scan(&rkey)
	s.DeliverAnnounce(reposter, repost, publicOrig)
	var rkey2 string
	db.QueryRow(`SELECT rkey FROM atproto_posts WHERE post_id = $1`, repost).Scan(&rkey2)
	if rkey != rkey2 {
		t.Error("a repeated DeliverAnnounce wrote a second record")
	}

	// Unreposting takes it down.
	s.DeliverUnannounce(reposter, repost, publicOrig)
	if sent(repost) {
		t.Error("deleting the repost left its Bluesky post behind")
	}

	// A repost with a narrower audience is not published to the world.
	friendsRepost := mkPost(reposter, "", "friends", &publicOrig)
	s.DeliverAnnounce(reposter, friendsRepost, publicOrig)
	if sent(friendsRepost) {
		t.Error("a friends-only repost was federated")
	}

	// An original that is not public never leaves its audience.
	for name, orig := range map[string]string{"friends-only": friendsOrig, "friend-list": listOrig} {
		r := mkPost(reposter, "", "public", &orig)
		s.DeliverAnnounce(reposter, r, orig)
		if sent(r) {
			t.Errorf("a repost of a %s post was federated", name)
		}
	}

	// A reposter who is not on Bluesky sends nothing.
	other := mkUser("other", false)
	otherRepost := mkPost(other, "", "public", &publicOrig)
	s.DeliverAnnounce(other, otherRepost, publicOrig)
	if sent(otherRepost) {
		t.Error("a repost by a user without AT Proto enabled was federated")
	}
}
