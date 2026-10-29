package atproto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-384: replies on a post ingested from Bluesky were never fetched, only
// replies on Agora's own posts were, so a reply from anyone this instance
// doesn't follow (another Agora instance's, say) never showed up.

// fakeThreadAppView serves app.bsky.feed.getPostThread from a uri -> thread
// map and counts the requests per uri.
type fakeThreadAppView struct {
	mu       sync.Mutex
	threads  map[string]map[string]any
	requests map[string]int
}

func newFakeThreadAppView(t *testing.T, db *store.DB) *fakeThreadAppView {
	t.Helper()
	f := &fakeThreadAppView{threads: map[string]map[string]any{}, requests: map[string]int{}}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xrpc/app.bsky.feed.getPostThread" {
			http.NotFound(w, r)
			return
		}
		uri := r.URL.Query().Get("uri")
		f.mu.Lock()
		f.requests[uri]++
		thread, ok := f.threads[uri]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "NotFound"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"thread": thread})
	}))
	t.Cleanup(srv.Close)

	prevClient := relayHTTPClient
	relayHTTPClient = srv.Client()
	t.Cleanup(func() { relayHTTPClient = prevClient })

	var prevHost string
	db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'atproto_appview_host'`).Scan(&prevHost)
	db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('atproto_appview_host', $1)
	         ON CONFLICT (key) DO UPDATE SET value = $1`, srv.Listener.Addr().String())
	t.Cleanup(func() {
		db.Exec(`UPDATE instance_settings SET value = $1 WHERE key = 'atproto_appview_host'`, prevHost)
	})
	return f
}

func (f *fakeThreadAppView) count(uri string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[uri]
}

func threadPost(uri, did, handle, text string) map[string]any {
	return map[string]any{
		"uri":       uri,
		"cid":       "bafyreib2rxk3rh6kzwq3x3tx6lql6tt2ymyuzbmhhu6zrhnpgbbjmr3y7a",
		"author":    map[string]any{"did": did, "handle": handle},
		"record":    map[string]any{"$type": "app.bsky.feed.post", "text": text, "createdAt": time.Now().UTC().Format(time.RFC3339)},
		"indexedAt": time.Now().UTC().Format(time.RFC3339),
	}
}

func threadNode(post map[string]any, replies ...map[string]any) map[string]any {
	rs := []any{}
	for _, r := range replies {
		rs = append(rs, r)
	}
	return map[string]any{"$type": "app.bsky.feed.defs#threadViewPost", "post": post, "replies": rs}
}

// seedIngestedRoot creates a Bluesky author stub and a top-level post
// ingested from them, published age ago.
func seedIngestedRoot(t *testing.T, db *store.DB, age time.Duration) (postID, uri string) {
	t.Helper()
	n := time.Now().UnixNano()
	did := fmt.Sprintf("did:plc:agora384author%d", n)
	var authorID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, is_remote, remote_instance, atproto_remote_did, profile_private)
		VALUES ($1, $2, '', true, 'bsky.app', $3, false) RETURNING id
	`, fmt.Sprintf("agora384_%d.bsky.social", n), fmt.Sprintf("agora384_%d@bsky.invalid", n), did).Scan(&authorID); err != nil {
		t.Fatalf("seed author: %v", err)
	}
	uri = fmt.Sprintf("at://%s/app.bsky.feed.post/root%d", did, n)
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, is_remote, remote_post_id, remote_instance, published_at, created_at)
		VALUES ($1, 'a post on Bluesky', 'public', true, $2, 'bsky.app', NOW() - make_interval(secs => $3), NOW() - make_interval(secs => $3))
		RETURNING id
	`, authorID, uri, age.Seconds()).Scan(&postID); err != nil {
		t.Fatalf("seed root post: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM posts WHERE parent_id = $1`, postID)
		db.Exec(`DELETE FROM posts WHERE id = $1`, postID)
		db.Exec(`DELETE FROM users WHERE id = $1`, authorID)
	})
	return postID, uri
}

func commentsUnder(t *testing.T, db *store.DB, postID string) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT id, COALESCE(remote_post_id, '') FROM posts WHERE parent_id = $1 AND deleted_at IS NULL`, postID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, uri string
		rows.Scan(&id, &uri)
		out[id] = uri
	}
	return out
}

func TestSyncBlueskyThread(t *testing.T) {
	db := testDB(t)
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	fake := newFakeThreadAppView(t, db)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora384.example"}}

	rootID, rootURI := seedIngestedRoot(t, db, time.Hour)
	n := time.Now().UnixNano()

	// This instance's own reply to the thread, already sent to Bluesky, which
	// the AppView hands back as a thread node like any other.
	localDID := fmt.Sprintf("did:web:local%d.agora384.example", n)
	var localUserID, localCommentID string
	if err := db.QueryRow(`INSERT INTO users (username, email, password_hash, atproto_did) VALUES ($1, $2, '', $3) RETURNING id`,
		fmt.Sprintf("agora384local_%d", n), fmt.Sprintf("agora384local_%d@example.invalid", n), localDID).Scan(&localUserID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, localUserID) })
	if err := db.QueryRow(`INSERT INTO posts (author_id, content, visibility, parent_id) VALUES ($1, 'my reply', 'public', $2) RETURNING id`,
		localUserID, rootID).Scan(&localCommentID); err != nil {
		t.Fatal(err)
	}
	rkey := fmt.Sprintf("local%d", n)
	db.Exec(`INSERT INTO atproto_posts (post_id, user_id, rkey, record_cid) VALUES ($1, $2, $3, 'bafy')`, localCommentID, localUserID, rkey)
	ownReplyURI := "at://" + localDID + "/app.bsky.feed.post/" + rkey

	otherDID := fmt.Sprintf("did:web:other%d.agora384.example", n)
	otherReplyURI := fmt.Sprintf("at://%s/app.bsky.feed.post/other%d", otherDID, n)
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE atproto_remote_did = $1`, otherDID) })

	fake.threads[rootURI] = threadNode(threadPost(rootURI, "did:plc:x", "teddy.bsky.social", "a post on Bluesky"),
		threadNode(threadPost(ownReplyURI, localDID, "koree.local.example", "my reply")),
		threadNode(threadPost(otherReplyURI, otherDID, "koree.other.example", "CAT")),
	)

	s.SyncBlueskyThread(rootID)

	got := commentsUnder(t, db, rootID)
	if len(got) != 2 {
		t.Fatalf("comments under the thread = %v, want this instance's own reply plus the other server's", got)
	}
	var sawOther bool
	for id, uri := range got {
		if uri == otherReplyURI {
			sawOther = true
		}
		if uri == ownReplyURI {
			t.Errorf("own reply was ingested again as %s", id)
		}
	}
	if !sawOther {
		t.Errorf("the other server's reply %s was not ingested", otherReplyURI)
	}

	// Opening it again straight away doesn't go back to the AppView.
	s.SyncBlueskyThread(rootID)
	if c := fake.count(rootURI); c != 1 {
		t.Errorf("AppView asked %d times, want 1 within the cooldown", c)
	}

	t.Run("a post that isn't a Bluesky thread is left alone", func(t *testing.T) {
		var localPostID string
		if err := db.QueryRow(`INSERT INTO posts (author_id, content, visibility) VALUES ($1, 'local only', 'public') RETURNING id`, localUserID).Scan(&localPostID); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, localPostID) })
		before := len(fake.requests)
		s.SyncBlueskyThread(localPostID)
		if len(fake.requests) != before {
			t.Error("asked the AppView about a post that never came from or went to Bluesky")
		}
	})
}

func TestClaimThreadSyncCooldown(t *testing.T) {
	id := fmt.Sprintf("post-%d", time.Now().UnixNano())
	now := time.Now()
	if !claimThreadSync(id, now) {
		t.Fatal("first claim refused")
	}
	if claimThreadSync(id, now.Add(threadSyncCooldown/2)) {
		t.Error("second claim within the cooldown allowed")
	}
	if !claimThreadSync(id, now.Add(threadSyncCooldown+time.Second)) {
		t.Error("claim after the cooldown refused")
	}
}

func TestPollInboundRepliesCoversRecentIngestedThreads(t *testing.T) {
	db := testDB(t)
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	fake := newFakeThreadAppView(t, db)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora384.example"}}

	_, recentURI := seedIngestedRoot(t, db, 2*time.Hour)
	_, oldURI := seedIngestedRoot(t, db, ingestedThreadPollWindow+24*time.Hour)

	s.pollInboundReplies(context.Background())

	if fake.count(recentURI) != 1 {
		t.Errorf("recent ingested thread polled %d times, want 1", fake.count(recentURI))
	}
	if fake.count(oldURI) != 0 {
		t.Errorf("thread older than the window polled %d times, want 0", fake.count(oldURI))
	}
}
