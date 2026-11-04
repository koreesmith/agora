package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
)

// AGORA-453: a quote-share from another Agora instance has to show as a quote.

func TestSplitQuoteLine(t *testing.T) {
	cases := []struct{ in, url, rest string }{
		{"@james Take note.\n\nRE: https://bsky.app/profile/did:plc:abc/post/3k", "https://bsky.app/profile/did:plc:abc/post/3k", "@james Take note."},
		{"hi\n\nRE: https://example.com/a/1\n", "https://example.com/a/1", "hi"},
		{"RE: https://example.com/a/1", "https://example.com/a/1", ""},
		{"no quote here", "", "no quote here"},
		{"RE: https://example.com/a/1\nand more text after", "", "RE: https://example.com/a/1\nand more text after"}, // not the last line
		{"re the thing: https://example.com", "", "re the thing: https://example.com"},
	}
	for _, c := range cases {
		u, rest := splitQuoteLine(c.in)
		if u != c.url || rest != c.rest {
			t.Errorf("splitQuoteLine(%q) = (%q, %q), want (%q, %q)", c.in, u, rest, c.url, c.rest)
		}
	}
}

func TestQuotePropertiesDecode(t *testing.T) {
	read := func(raw string) string {
		var n struct{ apQuoteRefs }
		if err := json.Unmarshal([]byte(raw), &n); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
		return n.first()
	}
	if got := read(`{"quoteUrl":"https://a.example/p/1"}`); got != "https://a.example/p/1" {
		t.Errorf("quoteUrl: %q", got)
	}
	if got := read(`{"_misskey_quote":"https://a.example/p/2"}`); got != "https://a.example/p/2" {
		t.Errorf("_misskey_quote: %q", got)
	}
	// FEP-044f's quote can be an object; and a shape we don't know is no quote,
	// not a decode failure that drops the whole post.
	if got := read(`{"quote":{"id":"https://a.example/p/3","type":"Note"}}`); got != "https://a.example/p/3" {
		t.Errorf("quote object: %q", got)
	}
	if got := read(`{"quote":42,"quoteUri":["x"]}`); got != "" {
		t.Errorf("odd shape should be no quote, got %q", got)
	}
	if got := read(`{"quoteUrl":"http://insecure.example/p"}`); got != "" {
		t.Errorf("only https quotes: %q", got)
	}
}

func TestBlueskyWebURLShape(t *testing.T) {
	m := bskyWebPost.FindStringSubmatch("https://bsky.app/profile/did:plc:yfyg7lj7atao6krdbaqaci35/post/3mzifv7ttsk2x")
	if m == nil || m[1] != "did:plc:yfyg7lj7atao6krdbaqaci35" || m[2] != "3mzifv7ttsk2x" {
		t.Fatalf("did url not matched: %v", m)
	}
	if bskyWebPost.MatchString("https://bsky.app/profile/someone.bsky.social/post/3mzifv7ttsk2x") {
		t.Error("a handle url has no DID to look up and must not match")
	}
}

type fakeQuoteBsky struct {
	called int
	id     string
}

func (f *fakeQuoteBsky) LookupBlueskyUser(ctx context.Context, handle string) (string, error) {
	return "", nil
}
func (f *fakeQuoteBsky) IngestPostByURI(ctx context.Context, atURI string) (string, error) {
	f.called++
	return f.id, nil
}

func TestLinkInboundQuote(t *testing.T) {
	db := openMetaTestDB(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora.example"}}
	tag := fmt.Sprintf("%d", time.Now().UnixNano())

	mkUser := func(name string, remote bool) string {
		var id string
		n := fmt.Sprintf("q453_%s_%s", name, tag)
		if err := db.QueryRow(`INSERT INTO users (username, email, password_hash, is_remote) VALUES ($1,$2,'x',$3) RETURNING id`, n, n+"@example.invalid", remote).Scan(&id); err != nil {
			t.Fatalf("user: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	mkPost := func(author, content, vis string, remote bool, remoteID, instance string) string {
		var id string
		if err := db.QueryRow(`INSERT INTO posts (author_id, content, visibility, is_remote, remote_post_id, remote_instance) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			author, content, vis, remote, remoteID, instance).Scan(&id); err != nil {
			t.Fatalf("post: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE repost_of_id = $1 OR id = $1`, id) })
		return id
	}
	read := func(id string) (content string, of *string) {
		db.QueryRow(`SELECT content, repost_of_id FROM posts WHERE id = $1`, id).Scan(&content, &of)
		return
	}

	local := mkUser("local", false)
	remote := mkUser("remote", true)
	pubPost := mkPost(local, "a public post", "public", false, "", "")
	friendsPost := mkPost(local, "a friends post", "friends", false, "", "")
	bskyURI := "at://did:plc:abc123/app.bsky.feed.post/3kabc"
	bskyPost := mkPost(remote, "from bluesky", "public", true, bskyURI, "bsky.app")
	apURL := "https://other.example/users/x/statuses/9"
	apPost := mkPost(remote, "from mastodon", "public", true, apURL, "other.example")

	t.Run("a local post named by its url", func(t *testing.T) {
		in := mkPost(remote, "great.\n\nRE: https://agora.example/federation/users/u/posts/"+pubPost, "public", true, "https://r.example/n/1"+tag, "r.example")
		s.linkInboundQuote(in, "")
		c, of := read(in)
		if of == nil || *of != pubPost {
			t.Fatalf("repost_of_id = %v, want %s", of, pubPost)
		}
		if c != "great." {
			t.Errorf("the RE line should go once linked, got %q", c)
		}
	})

	t.Run("a friends-only local post is never shown as someone's quote", func(t *testing.T) {
		in := mkPost(remote, "peek\n\nRE: https://agora.example/federation/users/u/posts/"+friendsPost, "public", true, "https://r.example/n/2"+tag, "r.example")
		s.linkInboundQuote(in, "")
		c, of := read(in)
		if of != nil {
			t.Fatal("a non-public post must not be linked")
		}
		if c == "peek" {
			t.Error("text must be left as it arrived when nothing was linked")
		}
	})

	t.Run("a bluesky post already stored", func(t *testing.T) {
		in := mkPost(remote, "take note\n\nRE: https://bsky.app/profile/did:plc:abc123/post/3kabc", "public", true, "https://r.example/n/3"+tag, "r.example")
		s.linkInboundQuote(in, "")
		c, of := read(in)
		if of == nil || *of != bskyPost || c != "take note" {
			t.Fatalf("got (%q, %v), want quote of %s", c, of, bskyPost)
		}
	})

	t.Run("a bluesky post not yet stored is fetched", func(t *testing.T) {
		fresh := mkPost(remote, "fetched later", "public", true, "at://did:plc:zzz/app.bsky.feed.post/3knew", "bsky.app")
		fake := &fakeQuoteBsky{id: fresh}
		s2 := &Service{db: db, cfg: s.cfg, bsky: fake}
		in := mkPost(remote, "see\n\nRE: https://bsky.app/profile/did:plc:zzz/post/3knew", "public", true, "https://r.example/n/4"+tag, "r.example")
		// the stub row is removed so the lookup has to go through the fetch
		db.Exec(`UPDATE posts SET remote_post_id = 'at://gone/' || id WHERE id = $1`, fresh)
		s2.linkInboundQuote(in, "")
		_, of := read(in)
		if fake.called != 1 || of == nil || *of != fresh {
			t.Fatalf("called %d times, repost_of_id %v, want a fetch and a link to %s", fake.called, of, fresh)
		}
	})

	t.Run("an explicit quote property wins over the text", func(t *testing.T) {
		in := mkPost(remote, "hello", "public", true, "https://r.example/n/5"+tag, "r.example")
		s.linkInboundQuote(in, apURL)
		c, of := read(in)
		if of == nil || *of != apPost || c != "hello" {
			t.Fatalf("got (%q, %v), want quote of %s with text kept", c, of, apPost)
		}
	})

	t.Run("an unknown quote leaves the post alone", func(t *testing.T) {
		in := mkPost(remote, "x\n\nRE: https://nowhere.example/p/1", "public", true, "https://r.example/n/6"+tag, "r.example")
		s.linkInboundQuote(in, "")
		c, of := read(in)
		if of != nil || c != "x\n\nRE: https://nowhere.example/p/1" {
			t.Fatalf("expected no change, got (%q, %v)", c, of)
		}
	})

	t.Run("a quote of a repost is not followed", func(t *testing.T) {
		rp := mkPost(local, "reshare", "public", false, "", "")
		db.Exec(`UPDATE posts SET repost_of_id = $1 WHERE id = $2`, pubPost, rp)
		in := mkPost(remote, "y\n\nRE: https://agora.example/federation/users/u/posts/"+rp, "public", true, "https://r.example/n/7"+tag, "r.example")
		s.linkInboundQuote(in, "")
		if _, of := read(in); of != nil {
			t.Fatal("one level only: a repost is not a quote target")
		}
	})
}

// What arrives is the sender's HTML; stored text is HTMLToPlainText of it, and
// the RE: line has to survive that round trip intact for the link to be found.
func TestQuoteLineSurvivesHTMLToPlainText(t *testing.T) {
	html := `<p>@james Take note.</p><p>RE: <a href="https://bsky.app/profile/did:plc:abc/post/3k" rel="nofollow noopener noreferrer" target="_blank">https://bsky.app/profile/did:plc:abc/post/3k</a></p>`
	u, rest := splitQuoteLine(HTMLToPlainText(html))
	if u != "https://bsky.app/profile/did:plc:abc/post/3k" || rest != "@james Take note." {
		t.Fatalf("got (%q, %q) from %q", u, rest, HTMLToPlainText(html))
	}
	// and the way Agora itself sends it: line breaks, not paragraphs
	br := `@james Take note.<br><br>RE: <a href="https://bsky.app/profile/did:plc:abc/post/3k">https://bsky.app/profile/did:plc:abc/post/3k</a>`
	if u, _ := splitQuoteLine(HTMLToPlainText(br)); u == "" {
		t.Fatalf("no RE: line found in %q", HTMLToPlainText(br))
	}
}
