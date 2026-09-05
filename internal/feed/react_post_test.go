package feed

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/agora-social/agora/internal/ctxkeys"
	"github.com/agora-social/agora/internal/store"
)

// fakeFedSender/fakeAtprotoSender satisfy fedSender/atprotoSender with every
// method a no-op except DeliverLike/DeliverUnlike, which record the call on
// a buffered channel so a test can assert whether one happened (and wait for
// the `go` call site without a sleep).
type fakeFedSender struct {
	likeCalls, unlikeCalls chan string
}

func newFakeFedSender() *fakeFedSender {
	return &fakeFedSender{likeCalls: make(chan string, 10), unlikeCalls: make(chan string, 10)}
}

func (f *fakeFedSender) BroadcastPublicPost(userID, postID string)                 {}
func (f *fakeFedSender) BroadcastFriendsPost(userID, postID string)                {}
func (f *fakeFedSender) BroadcastListPost(userID, postID string)                   {}
func (f *fakeFedSender) BroadcastDeletePost(userID, postID string)                 {}
func (f *fakeFedSender) BroadcastUpdatePost(userID, postID string)                 {}
func (f *fakeFedSender) DeliverReply(userID, commentID, replyToID string)          {}
func (f *fakeFedSender) DeliverReplyUpdate(userID, commentID, replyToID string)    {}
func (f *fakeFedSender) BroadcastPagePostUpdate(pageID, postID string)             {}
func (f *fakeFedSender) BroadcastPagePostDelete(pageID, postID string)             {}
func (f *fakeFedSender) DeliverLike(userID, postID string)                         { f.likeCalls <- postID }
func (f *fakeFedSender) DeliverUnlike(userID, postID string)                       { f.unlikeCalls <- postID }
func (f *fakeFedSender) DeliverAnnounce(userID, repostID, originalPostID string)   {}
func (f *fakeFedSender) DeliverUnannounce(userID, repostID, originalPostID string) {}
func (f *fakeFedSender) DeliverVote(userID, postID, optionID string)               {}

type fakeAtprotoSender struct {
	likeCalls, unlikeCalls chan string
}

func newFakeAtprotoSender() *fakeAtprotoSender {
	return &fakeAtprotoSender{likeCalls: make(chan string, 10), unlikeCalls: make(chan string, 10)}
}

func (f *fakeAtprotoSender) BroadcastPost(userID, postID string)                       {}
func (f *fakeAtprotoSender) BroadcastPostUpdate(userID, postID string)                 {}
func (f *fakeAtprotoSender) BroadcastPostDelete(userID, postID string)                 {}
func (f *fakeAtprotoSender) DeliverReply(userID, commentID, replyToID string)          {}
func (f *fakeAtprotoSender) DeliverReplyUpdate(userID, commentID, replyToID string)    {}
func (f *fakeAtprotoSender) DeliverLike(userID, postID string)                         { f.likeCalls <- postID }
func (f *fakeAtprotoSender) DeliverUnlike(userID, postID string)                       { f.unlikeCalls <- postID }
func (f *fakeAtprotoSender) DeliverAnnounce(userID, repostID, originalPostID string)   {}
func (f *fakeAtprotoSender) DeliverUnannounce(userID, repostID, originalPostID string) {}

func expectCall(t *testing.T, ch chan string, want bool, what string) {
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

// AGORA-359: ActivityPub and Bluesky only have a plain Like, so ReactPost
// federates a positive/neutral reaction as one (better than nothing) but
// never a negative one (sad/angry/dislike — mapping those to a Like would
// misrepresent what actually happened). It also must retract a Like when a
// reaction moves off the federating side, and must not resend or re-retract
// when it just moves between two reactions on the same side.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestReactPostFederationByValence(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	newUserAndPost := func(t *testing.T) (userID, postID string) {
		t.Helper()
		suffix := fmt.Sprintf("%d", time.Now().UnixNano())
		if err := db.QueryRow(`
			INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')
			RETURNING id
		`, "agora359_"+suffix, "agora359_"+suffix+"@example.com").Scan(&userID); err != nil {
			t.Fatalf("insert user: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

		// Reacts to their own post throughout, so ReactPost's notify branch
		// (authorID != userID) never fires and s.notif can stay nil.
		if err := db.QueryRow(`
			INSERT INTO posts (author_id, content, visibility) VALUES ($1, 'agora359 test post', 'public')
			RETURNING id
		`, userID).Scan(&postID); err != nil {
			t.Fatalf("insert post: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })
		return userID, postID
	}

	react := func(t *testing.T, s *Service, userID, postID, reactionType string) {
		t.Helper()
		req := httptest.NewRequest("POST", "/posts/"+postID+"/react", strings.NewReader(`{"type":"`+reactionType+`"}`))
		req = req.WithContext(context.WithValue(req.Context(), ctxkeys.UserID, userID))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", postID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		s.ReactPost(w, req)
		if w.Code != 200 {
			t.Fatalf("ReactPost(%q) status = %d, want 200; body: %s", reactionType, w.Code, w.Body.String())
		}
	}

	t.Run("fresh reaction with a federating type sends a Like", func(t *testing.T) {
		userID, postID := newUserAndPost(t)
		fed, atp := newFakeFedSender(), newFakeAtprotoSender()
		s := &Service{db: db, fed: fed, atproto: atp}

		react(t, s, userID, postID, "pride")

		expectCall(t, fed.likeCalls, true, "fed DeliverLike")
		expectCall(t, atp.likeCalls, true, "atproto DeliverLike")
		expectCall(t, fed.unlikeCalls, false, "fed DeliverUnlike")
	})

	t.Run("fresh reaction with a negative type sends nothing", func(t *testing.T) {
		userID, postID := newUserAndPost(t)
		fed, atp := newFakeFedSender(), newFakeAtprotoSender()
		s := &Service{db: db, fed: fed, atproto: atp}

		react(t, s, userID, postID, "sad")

		expectCall(t, fed.likeCalls, false, "fed DeliverLike")
		expectCall(t, atp.likeCalls, false, "atproto DeliverLike")
	})

	t.Run("switching between two federating reactions sends nothing further", func(t *testing.T) {
		userID, postID := newUserAndPost(t)
		fed, atp := newFakeFedSender(), newFakeAtprotoSender()
		s := &Service{db: db, fed: fed, atproto: atp}

		react(t, s, userID, postID, "love")
		expectCall(t, fed.likeCalls, true, "fed DeliverLike")
		expectCall(t, atp.likeCalls, true, "atproto DeliverLike")

		react(t, s, userID, postID, "pride")
		expectCall(t, fed.likeCalls, false, "fed DeliverLike (re-send)")
		expectCall(t, fed.unlikeCalls, false, "fed DeliverUnlike")
		expectCall(t, atp.likeCalls, false, "atproto DeliverLike (re-send)")
		expectCall(t, atp.unlikeCalls, false, "atproto DeliverUnlike")
	})

	t.Run("switching from a federating to a negative reaction retracts the Like", func(t *testing.T) {
		userID, postID := newUserAndPost(t)
		fed, atp := newFakeFedSender(), newFakeAtprotoSender()
		s := &Service{db: db, fed: fed, atproto: atp}

		react(t, s, userID, postID, "like")
		expectCall(t, fed.likeCalls, true, "fed DeliverLike")
		expectCall(t, atp.likeCalls, true, "atproto DeliverLike")

		react(t, s, userID, postID, "angry")
		expectCall(t, fed.unlikeCalls, true, "fed DeliverUnlike")
		expectCall(t, atp.unlikeCalls, true, "atproto DeliverUnlike")
		expectCall(t, fed.likeCalls, false, "fed DeliverLike (unexpected)")
	})

	t.Run("switching between two negative reactions sends nothing", func(t *testing.T) {
		userID, postID := newUserAndPost(t)
		fed, atp := newFakeFedSender(), newFakeAtprotoSender()
		s := &Service{db: db, fed: fed, atproto: atp}

		react(t, s, userID, postID, "sad")
		react(t, s, userID, postID, "angry")

		expectCall(t, fed.likeCalls, false, "fed DeliverLike")
		expectCall(t, fed.unlikeCalls, false, "fed DeliverUnlike")
		expectCall(t, atp.likeCalls, false, "atproto DeliverLike")
		expectCall(t, atp.unlikeCalls, false, "atproto DeliverUnlike")
	})

	t.Run("a prior legacy plain like row is treated as a federated like", func(t *testing.T) {
		userID, postID := newUserAndPost(t)
		fed, atp := newFakeFedSender(), newFakeAtprotoSender()
		s := &Service{db: db, fed: fed, atproto: atp}

		if _, err := db.Exec(`INSERT INTO likes (user_id, post_id) VALUES ($1, $2)`, userID, postID); err != nil {
			t.Fatalf("insert legacy like row: %v", err)
		}

		react(t, s, userID, postID, "angry")
		expectCall(t, fed.unlikeCalls, true, "fed DeliverUnlike")
		expectCall(t, atp.unlikeCalls, true, "atproto DeliverUnlike")
		expectCall(t, fed.likeCalls, false, "fed DeliverLike (unexpected)")
	})
}
