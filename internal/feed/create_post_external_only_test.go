package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/ctxkeys"
	"github.com/agora-social/agora/internal/store"
)

// recordingFedSender/recordingAtprotoSender record whether their Create-path
// method was called (AGORA-371), on a buffered channel so a test can assert
// without a sleep. Every other method is a no-op — this file only cares
// about CreatePost's own dispatch, same as fakeFedSender/fakeAtprotoSender in
// react_post_test.go, just recording a different pair of calls.
type recordingFedSender struct{ broadcasts chan string }

func (f *recordingFedSender) BroadcastPublicPost(userID, postID string)                 { f.broadcasts <- postID }
func (f *recordingFedSender) BroadcastFriendsPost(userID, postID string)                {}
func (f *recordingFedSender) BroadcastListPost(userID, postID string)                   {}
func (f *recordingFedSender) BroadcastDeletePost(userID, postID string)                 {}
func (f *recordingFedSender) BroadcastUpdatePost(userID, postID string)                 {}
func (f *recordingFedSender) DeliverReply(userID, commentID, replyToID string)          {}
func (f *recordingFedSender) DeliverReplyUpdate(userID, commentID, replyToID string)    {}
func (f *recordingFedSender) BroadcastPagePostUpdate(pageID, postID string)             {}
func (f *recordingFedSender) BroadcastPagePostDelete(pageID, postID string)             {}
func (f *recordingFedSender) DeliverLike(userID, postID, reactionType string)           {}
func (f *recordingFedSender) DeliverUnlike(userID, postID string)                       {}
func (f *recordingFedSender) DeliverAnnounce(userID, repostID, originalPostID string)   {}
func (f *recordingFedSender) DeliverUnannounce(userID, repostID, originalPostID string) {}
func (f *recordingFedSender) DeliverVote(userID, postID, optionID string)               {}

type recordingAtprotoSender struct{ broadcasts chan string }

func (f *recordingAtprotoSender) BroadcastPost(userID, postID string)                     { f.broadcasts <- postID }
func (f *recordingAtprotoSender) BroadcastPostUpdate(userID, postID string)               {}
func (f *recordingAtprotoSender) BroadcastPostDelete(userID, postID string)               {}
func (f *recordingAtprotoSender) DeliverReply(userID, commentID, replyToID string)        {}
func (f *recordingAtprotoSender) DeliverReplyUpdate(userID, commentID, replyToID string)  {}
func (f *recordingAtprotoSender) DeliverLike(userID, postID string)                       {}
func (f *recordingAtprotoSender) DeliverUnlike(userID, postID string)                     {}
func (f *recordingAtprotoSender) DeliverAnnounce(userID, repostID, originalPostID string) {}
func (f *recordingAtprotoSender) DeliverUnannounce(userID, repostID, originalPostID string) {
}

func expectBroadcast(t *testing.T, ch chan string, want bool, what string) {
	t.Helper()
	select {
	case <-ch:
		if !want {
			t.Errorf("unexpected %s call", what)
		}
	case <-time.After(300 * time.Millisecond):
		if want {
			t.Errorf("expected a %s call, none arrived", what)
		}
	}
}

// AGORA-371: external_only posts deliver outbound to exactly the requested
// external network(s), are stored visibility = 'private' + external_only =
// true (so they inherit every existing feed/profile/search/hashtag/
// notification exclusion that already keys off 'private'), generate no
// post_followers notification, and are rejected outright with no external
// target selected.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestCreatePostExternalOnly(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	newUser := func(t *testing.T) string {
		t.Helper()
		suffix := fmt.Sprintf("%d", time.Now().UnixNano())
		var userID string
		if err := db.QueryRow(`
			INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')
			RETURNING id
		`, "agora371_"+suffix, "agora371_"+suffix+"@example.com").Scan(&userID); err != nil {
			t.Fatalf("insert user: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })
		return userID
	}

	create := func(t *testing.T, s *Service, userID, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("POST", "/posts", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), ctxkeys.UserID, userID))
		w := httptest.NewRecorder()
		s.CreatePost(w, req)
		return w
	}

	t.Run("fediverse-only delivers to fed but not atproto, and is stored hidden", func(t *testing.T) {
		userID := newUser(t)
		fed := &recordingFedSender{broadcasts: make(chan string, 1)}
		atp := &recordingAtprotoSender{broadcasts: make(chan string, 1)}
		s := &Service{db: db, fed: fed, atproto: atp}

		w := create(t, s, userID, `{"content":"fediverse only, please","visibility":"public","external_only":true,"federate_activitypub":true,"federate_atproto":false}`)
		if w.Code != 201 {
			t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
		}
		var resp struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, resp.ID) })

		expectBroadcast(t, fed.broadcasts, true, "fed.BroadcastPublicPost")
		expectBroadcast(t, atp.broadcasts, false, "atproto.BroadcastPost")

		var visibility string
		var externalOnly bool
		if err := db.QueryRow(`SELECT visibility, external_only FROM posts WHERE id = $1`, resp.ID).Scan(&visibility, &externalOnly); err != nil {
			t.Fatalf("query stored post: %v", err)
		}
		if visibility != "private" || !externalOnly {
			t.Fatalf("stored (visibility, external_only) = (%q, %v), want (\"private\", true)", visibility, externalOnly)
		}

		var followerNotifs int
		db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE type = 'post_followers' AND post_id = $1`, resp.ID).Scan(&followerNotifs)
		if followerNotifs != 0 {
			t.Fatalf("expected no post_followers notifications, got %d", followerNotifs)
		}
	})

	t.Run("both networks selected delivers to both", func(t *testing.T) {
		userID := newUser(t)
		fed := &recordingFedSender{broadcasts: make(chan string, 1)}
		atp := &recordingAtprotoSender{broadcasts: make(chan string, 1)}
		s := &Service{db: db, fed: fed, atproto: atp}

		w := create(t, s, userID, `{"content":"both, please","visibility":"public","external_only":true,"federate_activitypub":true,"federate_atproto":true}`)
		if w.Code != 201 {
			t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
		}
		var resp struct {
			ID string `json:"id"`
		}
		json.Unmarshal(w.Body.Bytes(), &resp)
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, resp.ID) })

		expectBroadcast(t, fed.broadcasts, true, "fed.BroadcastPublicPost")
		expectBroadcast(t, atp.broadcasts, true, "atproto.BroadcastPost")
	})

	t.Run("no external target is rejected 400", func(t *testing.T) {
		userID := newUser(t)
		fed := &recordingFedSender{broadcasts: make(chan string, 1)}
		atp := &recordingAtprotoSender{broadcasts: make(chan string, 1)}
		s := &Service{db: db, fed: fed, atproto: atp}

		w := create(t, s, userID, `{"content":"nowhere to go","visibility":"public","external_only":true,"federate_activitypub":false,"federate_atproto":false}`)
		if w.Code != 400 {
			t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
		}

		var count int
		db.QueryRow(`SELECT COUNT(*) FROM posts WHERE author_id = $1`, userID).Scan(&count)
		if count != 0 {
			t.Fatalf("expected no post to be created, found %d", count)
		}
	})

	t.Run("a normal public post is unaffected", func(t *testing.T) {
		userID := newUser(t)
		fed := &recordingFedSender{broadcasts: make(chan string, 1)}
		atp := &recordingAtprotoSender{broadcasts: make(chan string, 1)}
		s := &Service{db: db, fed: fed, atproto: atp}

		w := create(t, s, userID, `{"content":"just a normal post","visibility":"public"}`)
		if w.Code != 201 {
			t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
		}
		var resp struct {
			ID string `json:"id"`
		}
		json.Unmarshal(w.Body.Bytes(), &resp)
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, resp.ID) })

		expectBroadcast(t, fed.broadcasts, true, "fed.BroadcastPublicPost")
		expectBroadcast(t, atp.broadcasts, true, "atproto.BroadcastPost")

		var visibility string
		var externalOnly bool
		db.QueryRow(`SELECT visibility, external_only FROM posts WHERE id = $1`, resp.ID).Scan(&visibility, &externalOnly)
		if visibility != "public" || externalOnly {
			t.Fatalf("stored (visibility, external_only) = (%q, %v), want (\"public\", false)", visibility, externalOnly)
		}
	})
}
