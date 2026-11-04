package atproto

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/agora-social/agora/internal/config"
)

// AGORA-459

func TestWithWarning(t *testing.T) {
	for _, c := range []struct{ cw, text, want string }{
		{"", "hello", "hello"},
		{"  ", "hello", "hello"},
		{"politics", "hello", "CW: politics\n\nhello"},
		{" politics ", "hello", "CW: politics\n\nhello"},
		{"politics", "", "CW: politics"},
		{"politics", "  \n", "CW: politics"},
	} {
		if got := withWarning(c.cw, c.text); got != c.want {
			t.Errorf("withWarning(%q, %q) = %q, want %q", c.cw, c.text, got, c.want)
		}
	}
}

func TestQuoteRepostText(t *testing.T) {
	if got, want := quoteRepostText("ICE news", "  so true  "), "CW: ICE news\n\nso true"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := quoteRepostText("ICE news", ""); got != "CW: ICE news" {
		t.Errorf("warning only: %q", got)
	}
	long := quoteRepostText("w", strings.Repeat("word ", 200))
	if n := utf8.RuneCountInString(long); n > blueskyMaxGraphemes {
		t.Errorf("quote text is %d characters, over the %d limit", n, blueskyMaxGraphemes)
	}
	if !strings.HasPrefix(long, "CW: w") {
		t.Errorf("a long comment cut the warning: %q", long[:20])
	}
}

func TestFlattenedRepostCarriesTheWarningLine(t *testing.T) {
	link := "https://agora.example/post/abc"
	got := flattenRepostForBluesky(withWarning("spoilers", "so true"), "Ada", "hello", link)
	if want := "CW: spoilers\n\nso true\n\n🔁 Ada: hello\n\n" + link; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := flattenRepostForBluesky(withWarning("spoilers", ""), "Ada", "hello", link); !strings.HasPrefix(got, "CW: spoilers\n\n🔁 Ada") {
		t.Errorf("warning with no comment: %q", got)
	}
}

// A repost of a Bluesky post goes out as a quote post only when it has a
// warning or the sharer's own words; otherwise it stays a plain repost record.
// Requires the local agora-postgres-test instance; skips otherwise.
func TestDeliverAnnounceQuotesABlueskyPostWhenItHasWords(t *testing.T) {
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

	mkUser := func(tag string, remote bool) string {
		name := fmt.Sprintf("agora459_%s_%d", tag, unique)
		var id string
		if err := db.QueryRow(`
			INSERT INTO users (username, email, password_hash, profile_private, atproto_enabled, atproto_did, display_name, is_remote)
			VALUES ($1, $2, '', false, $3, $4, $5, $6) RETURNING id
		`, name, name+"@example.invalid", !remote, "did:web:"+name, "User "+tag, remote).Scan(&id); err != nil {
			t.Fatalf("seed user %s: %v", tag, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	reposter, bskyAuthor := mkUser("reposter", false), mkUser("bsky", true)

	var orig string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, is_remote, remote_instance, remote_post_id, remote_post_cid)
		VALUES ($1, 'a bluesky post', 'public', true, 'bsky.app', $2, 'bafyreigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi') RETURNING id
	`, bskyAuthor, fmt.Sprintf("at://did:plc:abc%d/app.bsky.feed.post/3kabc", unique)).Scan(&orig); err != nil {
		t.Fatalf("seed bluesky post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, orig) })

	mkRepost := func(content, cw, visibility string) string {
		var id string
		if err := db.QueryRow(`
			INSERT INTO posts (author_id, content, content_warning, visibility, repost_of_id) VALUES ($1, $2, $3, $4, $5) RETURNING id
		`, reposter, content, cw, visibility, orig).Scan(&id); err != nil {
			t.Fatalf("seed repost: %v", err)
		}
		t.Cleanup(func() {
			db.Exec(`DELETE FROM atproto_posts WHERE post_id = $1`, id)
			db.Exec(`DELETE FROM posts WHERE id = $1`, id)
		})
		return id
	}

	s := NewService(db, &config.Config{InstanceDomain: "https://agora.example"}, nil)
	quoted := func(id string) bool {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM atproto_posts WHERE post_id = $1`, id).Scan(&n)
		return n == 1
	}

	withCW := mkRepost("", "ICE news", "public")
	s.DeliverAnnounce(reposter, withCW, orig)
	if !quoted(withCW) {
		t.Fatal("a repost with a trigger warning was not sent as a quote post")
	}
	var rkey string
	db.QueryRow(`SELECT rkey FROM atproto_posts WHERE post_id = $1`, withCW).Scan(&rkey)
	s.DeliverAnnounce(reposter, withCW, orig)
	var rkey2 string
	db.QueryRow(`SELECT rkey FROM atproto_posts WHERE post_id = $1`, withCW).Scan(&rkey2)
	if rkey != rkey2 {
		t.Error("a repeated DeliverAnnounce wrote a second quote post")
	}
	s.DeliverUnannounce(reposter, withCW, orig)
	if quoted(withCW) {
		t.Error("undoing the repost left its quote post behind")
	}

	withWords := mkRepost("my take", "", "public")
	s.DeliverAnnounce(reposter, withWords, orig)
	if !quoted(withWords) {
		t.Error("a repost with the sharer's own comment was not sent as a quote post")
	}

	bare := mkRepost("", "", "public")
	s.DeliverAnnounce(reposter, bare, orig)
	if quoted(bare) {
		t.Error("a bare repost became a quote post; it should stay a plain repost record")
	}

	// A repost that is not public sends no words: it falls back to the plain
	// repost record, as before.
	friends := mkRepost("private thoughts", "CW", "friends")
	s.DeliverAnnounce(reposter, friends, orig)
	if quoted(friends) {
		t.Error("the words of a friends-only repost were published")
	}
}
