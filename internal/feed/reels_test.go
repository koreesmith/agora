package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/agora-social/agora/internal/ctxkeys"
	"github.com/agora-social/agora/internal/store"
)

type reelsEnv struct {
	t  *testing.T
	db *store.DB
	s  *Service
	n  int64
}

func newReelsEnv(t *testing.T) *reelsEnv {
	t.Helper()
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() }) // registered first, so it runs last
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &reelsEnv{t: t, db: db, s: &Service{db: db}, n: time.Now().UnixNano()}
}

func (e *reelsEnv) user(name string, private bool) string {
	var id string
	if err := e.db.QueryRow(`
		INSERT INTO users (username, email, password_hash, email_verified, profile_private)
		VALUES ($1, $2, 'x', true, $3) RETURNING id
	`, fmt.Sprintf("r438%s%d", name, e.n), fmt.Sprintf("r438%s%d@x.invalid", name, e.n), private).Scan(&id); err != nil {
		e.t.Fatalf("insert user %s: %v", name, err)
	}
	e.t.Cleanup(func() { e.db.Exec(`DELETE FROM users WHERE id = $1`, id) })
	return id
}

// video inserts a top-level post; minutesAgo orders them (bigger = older).
func (e *reelsEnv) video(author, visibility, video string, minutesAgo int) string {
	var id string
	if err := e.db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, video_url, published_at, created_at)
		VALUES ($1, 'a clip', $2, $3, NOW() - ($4 * INTERVAL '1 minute'), NOW() - ($4 * INTERVAL '1 minute'))
		RETURNING id
	`, author, visibility, video, minutesAgo).Scan(&id); err != nil {
		e.t.Fatalf("insert post: %v", err)
	}
	e.t.Cleanup(func() { e.db.Exec(`DELETE FROM posts WHERE id = $1`, id) })
	return id
}

type reelsPage struct {
	Posts      []Post `json:"posts"`
	NextCursor string `json:"next_cursor"`
	Exhausted  bool   `json:"exhausted"`
}

func (e *reelsEnv) get(viewer string, params url.Values) (int, reelsPage) {
	req := httptest.NewRequest("GET", "/reels?"+params.Encode(), nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxkeys.UserID, viewer))
	w := httptest.NewRecorder()
	e.s.GetReels(w, req)
	var page reelsPage
	json.Unmarshal(w.Body.Bytes(), &page)
	return w.Code, page
}

func (e *reelsEnv) ids(viewer string, params url.Values) []string {
	e.t.Helper()
	code, page := e.get(viewer, params)
	if code != 200 {
		e.t.Fatalf("GET /reels %v = %d", params, code)
	}
	out := []string{}
	for _, p := range page.Posts {
		out = append(out, p.ID)
	}
	return out
}

func (e *reelsEnv) viewed(viewer, postID string) int {
	req := httptest.NewRequest("POST", "/reels/"+postID+"/viewed", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", postID)
	req = req.WithContext(context.WithValue(context.WithValue(req.Context(), ctxkeys.UserID, viewer), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	e.s.MarkReelViewed(w, req)
	return w.Code
}

func sameIDs(got, want []string) bool { return fmt.Sprint(got) == fmt.Sprint(want) }

func (e *reelsEnv) want(name string, got, want []string) {
	e.t.Helper()
	if !sameIDs(got, want) {
		e.t.Errorf("%s:\n got  %v\n want %v", name, got, want)
	}
}

// The Following tab: your own, your friends' and your follows' videos, and
// nothing else, newest first, with everything the home feed hides left out.
func TestReelsFollowing(t *testing.T) {
	e := newReelsEnv(t)
	viewer, friend, stranger := e.user("viewer", false), e.user("friend", false), e.user("stranger", false)
	blocked, muted := e.user("blocked", false), e.user("muted", false)
	e.db.Exec(`INSERT INTO friendships (requester_id, addressee_id, status) VALUES ($1, $2, 'accepted')`, viewer, friend)
	e.db.Exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, viewer, blocked)
	e.t.Cleanup(func() { e.db.Exec(`DELETE FROM blocks WHERE blocker_id = $1`, viewer) })

	own := e.video(viewer, "friends", "/uploads/videos/own.mp4", 1)
	friendVid := e.video(friend, "friends", "/uploads/videos/f.mp4", 2)
	friendPublic := e.video(friend, "public", "/uploads/videos/fp.mp4", 3)
	e.video(stranger, "public", "/uploads/videos/s.mp4", 4)         // not followed
	e.video(blocked, "public", "/uploads/videos/b.mp4", 5)          // blocked
	hidden := e.video(friend, "public", "/uploads/videos/h.mp4", 6) // hidden by the viewer
	e.db.Exec(`INSERT INTO hidden_posts (user_id, post_id) VALUES ($1, $2)`, viewer, hidden)
	e.t.Cleanup(func() { e.db.Exec(`DELETE FROM hidden_posts WHERE user_id = $1`, viewer) })

	// a friend's text post, a friend's comment with a video, a deleted video
	e.db.Exec(`INSERT INTO posts (author_id, content, visibility) VALUES ($1, 'just words', 'public')`, friend)
	e.t.Cleanup(func() { e.db.Exec(`DELETE FROM posts WHERE author_id = $1 AND content = 'just words'`, friend) })
	deleted := e.video(friend, "public", "/uploads/videos/d.mp4", 7)
	e.db.Exec(`UPDATE posts SET deleted_at = NOW() WHERE id = $1`, deleted)
	comment := e.video(friend, "public", "/uploads/videos/c.mp4", 8)
	e.db.Exec(`UPDATE posts SET parent_id = $1 WHERE id = $2`, friendVid, comment)

	// a friend's friends-visibility video the viewer is not part of is simply
	// not theirs to see: the stranger's friends-only video
	e.video(stranger, "friends", "/uploads/videos/sf.mp4", 9)

	// a keyword the viewer muted, in a friend's video caption
	mutedVid := e.video(muted, "public", "/uploads/videos/m.mp4", 10)
	e.db.Exec(`INSERT INTO friendships (requester_id, addressee_id, status) VALUES ($1, $2, 'accepted')`, viewer, muted)
	e.db.Exec(`UPDATE posts SET content = 'spoilers ahead' WHERE id = $1`, mutedVid)
	e.db.Exec(`INSERT INTO keyword_mutes (user_id, keyword, pattern) VALUES ($1, 'spoilers', $2)`, viewer, keywordPattern("spoilers"))
	e.t.Cleanup(func() { e.db.Exec(`DELETE FROM keyword_mutes WHERE user_id = $1`, viewer) })

	e.want("following", e.ids(viewer, url.Values{"tab": {"following"}}), []string{own, friendVid, friendPublic})
	// "following" is the default tab
	e.want("default tab", e.ids(viewer, url.Values{}), []string{own, friendVid, friendPublic})
}

// A followed Fediverse or Bluesky account's videos join Following while its
// "show in feed" switch is on, exactly like the home feed.
func TestReelsFollowingRemoteFollows(t *testing.T) {
	e := newReelsEnv(t)
	viewer := e.user("viewer", false)
	apAuthor, atAuthor := e.user("apauthor", false), e.user("atauthor", false)

	actor := fmt.Sprintf("https://masto.example/users/r438_%d", e.n)
	did := fmt.Sprintf("did:plc:r438%d", e.n)
	e.db.Exec(`UPDATE users SET ap_actor_url = $2 WHERE id = $1`, apAuthor, actor)
	e.db.Exec(`UPDATE users SET atproto_remote_did = $2 WHERE id = $1`, atAuthor, did)
	e.db.Exec(`INSERT INTO ap_following (follower_user_id, followed_actor_url, followed_inbox_url, accepted, show_in_feed) VALUES ($1, $2, $2 || '/inbox', true, true)`, viewer, actor)
	e.db.Exec(`INSERT INTO at_following (local_user_id, remote_did, show_in_feed) VALUES ($1, $2, true)`, viewer, did)
	e.t.Cleanup(func() {
		e.db.Exec(`DELETE FROM ap_following WHERE follower_user_id = $1`, viewer)
		e.db.Exec(`DELETE FROM at_following WHERE local_user_id = $1`, viewer)
	})

	apVid := e.video(apAuthor, "public", "https://masto.example/v.mp4", 1)
	atVid := e.video(atAuthor, "public", "https://video.bsky.app/p.m3u8", 2)
	e.want("both follows shown", e.ids(viewer, url.Values{"tab": {"following"}}), []string{apVid, atVid})

	e.db.Exec(`UPDATE ap_following SET show_in_feed = false WHERE follower_user_id = $1`, viewer)
	e.want("ap follow muted from feed", e.ids(viewer, url.Values{"tab": {"following"}}), []string{atVid})
}

// Discover is Explore's pool narrowed to videos: public, from non-private
// accounts, from this instance or a peered Agora instance, not the viewer's
// own.
func TestReelsDiscover(t *testing.T) {
	e := newReelsEnv(t)
	viewer, author, privateAuthor := e.user("viewer", false), e.user("author", false), e.user("private", true)

	pub := e.video(author, "public", "/uploads/videos/p.mp4", 1)
	e.video(viewer, "public", "/uploads/videos/own.mp4", 2)       // own: not in Discover
	e.video(author, "friends", "/uploads/videos/fr.mp4", 3)       // not public
	e.video(privateAuthor, "public", "/uploads/videos/pr.mp4", 4) // private profile
	scheduled := e.user("leaving", false)
	e.video(scheduled, "public", "/uploads/videos/l.mp4", 5)
	e.db.Exec(`UPDATE users SET deletion_scheduled_at = NOW() WHERE id = $1`, scheduled)

	// remote: peered Agora instance in, unknown (Fediverse/Bluesky) and
	// blocked instance out
	peered := fmt.Sprintf("peer%d.example", e.n)
	blockedDomain := fmt.Sprintf("blocked%d.example", e.n)
	for d, status := range map[string]string{peered: "active", blockedDomain: "blocked"} {
		e.db.Exec(`INSERT INTO federated_instances (domain, name, instance_url, status, direction) VALUES ($1, $1, 'https://'||$1, $2, 'mutual')`, d, status)
		d := d
		e.t.Cleanup(func() { e.db.Exec(`DELETE FROM federated_instances WHERE domain = $1`, d) })
	}
	remoteAuthor := e.user("remote", false)
	remote := func(domain string, minutes int) string {
		id := e.video(remoteAuthor, "public", "https://"+domain+"/v.mp4", minutes)
		e.db.Exec(`UPDATE posts SET is_remote = true, remote_instance = $2, remote_post_id = $3 WHERE id = $1`, id, domain, "https://"+domain+"/p/"+id)
		return id
	}
	peeredVid := remote(peered, 6)
	remote(blockedDomain, 7)
	remote("mastodon.example", 8) // wider Fediverse: not in Discover

	e.want("discover", e.ids(viewer, url.Values{"tab": {"discover"}}), []string{pub, peeredVid})
}

// An admin can turn Discover off; Following is unaffected.
func TestReelsDiscoverSwitch(t *testing.T) {
	e := newReelsEnv(t)
	viewer := e.user("viewer", false)
	var prev string
	e.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'reels_discover_enabled'`).Scan(&prev)
	e.t.Cleanup(func() {
		e.db.Exec(`UPDATE instance_settings SET value = $1 WHERE key = 'reels_discover_enabled'`, prev)
	})

	if !e.s.reelsDiscoverEnabled() {
		t.Fatalf("Discover should default to on, got off (value %q)", prev)
	}
	e.db.Exec(`UPDATE instance_settings SET value = 'false' WHERE key = 'reels_discover_enabled'`)
	if code, _ := e.get(viewer, url.Values{"tab": {"discover"}}); code != 404 {
		t.Errorf("discover while off = %d, want 404", code)
	}
	if code, _ := e.get(viewer, url.Values{"tab": {"following"}}); code != 200 {
		t.Errorf("following while discover is off = %d, want 200", code)
	}
	e.db.Exec(`UPDATE instance_settings SET value = 'true' WHERE key = 'reels_discover_enabled'`)
	if code, _ := e.get(viewer, url.Values{"tab": {"discover"}}); code != 200 {
		t.Errorf("discover while on = %d, want 200", code)
	}
}

// Watched videos drop out, come back with include_viewed, and marking is
// idempotent and limited to videos the viewer could see.
func TestReelsViewed(t *testing.T) {
	e := newReelsEnv(t)
	viewer, author, stranger := e.user("viewer", false), e.user("author", false), e.user("stranger", false)
	e.db.Exec(`INSERT INTO friendships (requester_id, addressee_id, status) VALUES ($1, $2, 'accepted')`, viewer, author)
	e.t.Cleanup(func() { e.db.Exec(`DELETE FROM reel_views WHERE user_id = $1`, viewer) })

	a := e.video(author, "public", "/uploads/videos/a.mp4", 1)
	b := e.video(author, "public", "/uploads/videos/b.mp4", 2)
	following := url.Values{"tab": {"following"}}

	e.want("before", e.ids(viewer, following), []string{a, b})
	if code := e.viewed(viewer, a); code != 200 {
		t.Fatalf("mark viewed = %d", code)
	}
	if code := e.viewed(viewer, a); code != 200 {
		t.Errorf("marking twice = %d, want 200 (idempotent)", code)
	}
	e.want("after watching a", e.ids(viewer, following), []string{b})
	e.want("include_viewed", e.ids(viewer, url.Values{"tab": {"following"}, "include_viewed": {"true"}}), []string{a, b})

	// watching is per person
	if got := e.ids(author, url.Values{"tab": {"following"}}); !sameIDs(got, []string{a, b}) {
		t.Errorf("the author's own list changed: %v", got)
	}

	// everything watched: empty and exhausted
	e.viewed(viewer, b)
	code, page := e.get(viewer, following)
	if code != 200 || len(page.Posts) != 0 || !page.Exhausted || page.NextCursor != "" {
		t.Errorf("all watched: code %d, %d posts, exhausted=%v, cursor %q", code, len(page.Posts), page.Exhausted, page.NextCursor)
	}

	// not found: a missing post, a text post, a video the viewer can't see
	if code := e.viewed(viewer, "00000000-0000-0000-0000-000000000000"); code != 404 {
		t.Errorf("missing post = %d, want 404", code)
	}
	var text string
	e.db.QueryRow(`INSERT INTO posts (author_id, content, visibility) VALUES ($1, 'words', 'public') RETURNING id`, author).Scan(&text)
	e.t.Cleanup(func() { e.db.Exec(`DELETE FROM posts WHERE id = $1`, text) })
	if code := e.viewed(viewer, text); code != 404 {
		t.Errorf("text post = %d, want 404", code)
	}
	secret := e.video(stranger, "friends", "/uploads/videos/s.mp4", 3)
	if code := e.viewed(viewer, secret); code != 404 {
		t.Errorf("a friends-only video from a stranger = %d, want 404", code)
	}
}

// Cursor paging: no repeats, no gaps, correct exhausted flag, and marking a
// video watched between pages doesn't make the next page skip anything.
func TestReelsPaging(t *testing.T) {
	e := newReelsEnv(t)
	viewer, author := e.user("viewer", false), e.user("author", false)
	e.db.Exec(`INSERT INTO friendships (requester_id, addressee_id, status) VALUES ($1, $2, 'accepted')`, viewer, author)
	e.t.Cleanup(func() { e.db.Exec(`DELETE FROM reel_views WHERE user_id = $1`, viewer) })

	var all []string
	for i := 1; i <= 5; i++ {
		all = append(all, e.video(author, "public", fmt.Sprintf("/uploads/videos/p%d.mp4", i), i))
	}

	code, p1 := e.get(viewer, url.Values{"tab": {"following"}, "limit": {"2"}})
	if code != 200 || len(p1.Posts) != 2 || p1.NextCursor == "" || p1.Exhausted {
		t.Fatalf("page 1: code %d, %d posts, cursor %q, exhausted %v", code, len(p1.Posts), p1.NextCursor, p1.Exhausted)
	}
	// the viewer watches page 1, which must not disturb where page 2 starts
	for _, p := range p1.Posts {
		e.viewed(viewer, p.ID)
	}
	_, p2 := e.get(viewer, url.Values{"tab": {"following"}, "limit": {"2"}, "cursor": {p1.NextCursor}})
	_, p3 := e.get(viewer, url.Values{"tab": {"following"}, "limit": {"2"}, "cursor": {p2.NextCursor}})

	var got []string
	for _, pg := range []reelsPage{p1, p2, p3} {
		for _, p := range pg.Posts {
			got = append(got, p.ID)
		}
	}
	e.want("all five, in order, once each", got, all)
	if len(p2.Posts) != 2 || p2.Exhausted {
		t.Errorf("page 2: %d posts, exhausted %v", len(p2.Posts), p2.Exhausted)
	}
	if len(p3.Posts) != 1 || !p3.Exhausted || p3.NextCursor != "" {
		t.Errorf("page 3: %d posts, exhausted %v, cursor %q", len(p3.Posts), p3.Exhausted, p3.NextCursor)
	}
}

func TestReelsBadInput(t *testing.T) {
	e := newReelsEnv(t)
	viewer := e.user("viewer", false)
	for name, params := range map[string]url.Values{
		"unknown tab":    {"tab": {"trending"}},
		"garbage cursor": {"cursor": {"!!!"}},
		"cursor no id":   {"cursor": {"MjAyNi0xMS0wM1QxMDowMDowMFp8"}}, // "2026-11-03T10:00:00Z|"
	} {
		if code, _ := e.get(viewer, params); code != 400 {
			t.Errorf("%s: got %d, want 400", name, code)
		}
	}
	// limit is capped, not rejected
	if code, _ := e.get(viewer, url.Values{"limit": {"100000"}}); code != 200 {
		t.Errorf("huge limit: got %d, want 200", code)
	}
}

func TestReelCursorRoundTrip(t *testing.T) {
	ts := "2026-11-03T15:04:05.123456-06:00"
	id := "9b2c8f0e-1111-4222-8333-444455556666"
	gotT, gotID, ok := decodeReelCursor(encodeReelCursor(ts, id))
	if !ok || gotID != id || !gotT.Equal(mustParse(t, ts)) {
		t.Errorf("round trip: %v %q %v", gotT, gotID, ok)
	}
	for _, bad := range []string{"", "!!!", encodeReelCursor("yesterday", id), encodeReelCursor(ts, "")} {
		if _, _, ok := decodeReelCursor(bad); ok {
			t.Errorf("decodeReelCursor(%q) accepted a bad cursor", bad)
		}
	}
}

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
