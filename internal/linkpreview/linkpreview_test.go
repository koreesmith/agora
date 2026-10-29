package linkpreview

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

const sampleIndex = `<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <title>Agora</title>
    <meta name="description" content="A private, federated social network for the people you actually know." />
    <meta property="og:type" content="website" />
    <meta property="og:title" content="Agora" />
    <meta property="og:description" content="A private, federated social network for the people you actually know." />
    <meta property="og:image" content="/logo-circle.svg" />
    <meta name="twitter:card" content="summary" />
    <meta name="twitter:title" content="Agora" />
    <meta name="twitter:image" content="/logo-circle.svg" />
    <meta name="theme-color" content="#486581" />
    <script type="module" crossorigin src="/assets/index-abc123.js"></script>
  </head>
  <body><div id="root"></div></body>
</html>`

// AGORA-412: the post's own tags replace the generic ones exactly once each,
// everything else in the page (bundles, theme colour) is left alone, and
// post text can't break out of an attribute.
func TestInjectMetaReplacesTags(t *testing.T) {
	out := InjectMeta(sampleIndex, Meta{
		Title:       `Alex <script> "A"`,
		Description: `5 > 3 & "quotes"`,
		Image:       "https://a.example/uploads/x.jpg",
		URL:         "https://a.example/post/1",
		Type:        "article",
		LargeImage:  true,
	}, "Koree Social")

	for _, want := range []string{
		`<title>Alex &lt;script&gt; &#34;A&#34; on Koree Social</title>`,
		`<meta property="og:title" content="Alex &lt;script&gt; &#34;A&#34;" />`,
		`<meta property="og:description" content="5 &gt; 3 &amp; &#34;quotes&#34;" />`,
		`<meta property="og:image" content="https://a.example/uploads/x.jpg" />`,
		`<meta property="og:url" content="https://a.example/post/1" />`,
		`<meta name="twitter:card" content="summary_large_image" />`,
		`<meta property="og:site_name" content="Koree Social" />`,
		`<meta name="theme-color" content="#486581" />`,
		`src="/assets/index-abc123.js"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, tag := range []string{`property="og:title"`, `property="og:image"`, `name="twitter:card"`, `<title>`, `name="description"`} {
		if n := strings.Count(out, tag); n != 1 {
			t.Errorf("%s appears %d times, want 1", tag, n)
		}
	}
	if strings.Contains(out, "logo-circle.svg") {
		t.Error("generic logo image left in alongside the post's own")
	}
}

// A page with nothing specific to describe keeps the instance's own card.
func TestInjectMetaDefault(t *testing.T) {
	out := InjectMeta(sampleIndex, Meta{URL: "https://a.example/post/x", Type: "website"}, "Koree Social")
	for _, want := range []string{
		`<title>Koree Social</title>`,
		`<meta property="og:title" content="Koree Social" />`,
		`<meta property="og:image" content="/logo-circle.svg" />`,
		`<meta name="twitter:card" content="summary" />`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestSummarize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Hello\n\n  world", "Hello world"},
		{"<p>Hi &amp; <b>bye</b></p>", "Hi & bye"},
		{strings.Repeat("word ", 60), strings.TrimSpace(strings.Repeat("word ", 40)) + "…"},
	}
	for _, c := range cases {
		if got := Summarize(c.in, 200); got != c.want {
			t.Errorf("Summarize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The tags must never describe a post a signed-out visitor can't open.
// Requires agora-postgres-test (localhost:15433); skips if unreachable.
func TestPostMetaOnlyForPublicPosts(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := NewService(db, &config.Config{InstanceDomain: "https://a.example"})

	n := time.Now().UnixNano() % 1e12
	newUser := func(suffix string, private bool) string {
		var id string
		err := db.QueryRow(`INSERT INTO users (username, email, password_hash, display_name, profile_private)
			VALUES ($1, $2, 'x', 'Preview Test', $3) RETURNING id`,
			fmt.Sprintf("lp412%s%d", suffix, n), fmt.Sprintf("lp412%s%d@test.example", suffix, n), private).Scan(&id)
		if err != nil {
			t.Fatalf("seed user: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	newPost := func(author, visibility, cw string) string {
		var id string
		if err := db.QueryRow(`INSERT INTO posts (author_id, content, visibility, content_warning) VALUES ($1, 'hello preview', $2, $3) RETURNING id`,
			author, visibility, cw).Scan(&id); err != nil {
			t.Fatalf("seed post: %v", err)
		}
		return id
	}

	open := newUser("o", false)
	closed := newUser("p", true)

	public := newPost(open, "public", "")
	if m, ok := s.postMeta(context.Background(), public); !ok || m.Description != "hello preview" || !strings.HasPrefix(m.Title, "Preview Test (@lp412o") {
		t.Errorf("public post: ok=%v meta=%+v", ok, m)
	}
	if m, ok := s.postMeta(context.Background(), newPost(open, "public", "spoilers")); !ok || m.Description != "Content warning: spoilers" {
		t.Errorf("content warning: ok=%v meta=%+v", ok, m)
	}
	for name, id := range map[string]string{
		"friends-only":    newPost(open, "friends", ""),
		"private":         newPost(open, "private", ""),
		"private profile": newPost(closed, "public", ""),
	} {
		if _, ok := s.postMeta(context.Background(), id); ok {
			t.Errorf("%s post got preview tags", name)
		}
	}
	if _, ok := s.profileMeta(context.Background(), fmt.Sprintf("lp412p%d", n)); ok {
		t.Error("private profile got preview tags")
	}
}
