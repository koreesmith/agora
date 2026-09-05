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
// the `go` call site without a sleep). fakeFedSender's DeliverLike records
// the reaction type it was called with (AGORA-360) rather than just the
// postID, since which type reached it is exactly what these tests check —
// federation.Service.DeliverLike itself decides what, if anything, that
// turns into on the wire, which is federation package's own concern, not
// this one's.
type fakeFedSender struct {
	likeCalls   chan string // reaction type
	unlikeCalls chan string // postID
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
func (f *fakeFedSender) DeliverLike(userID, postID, reactionType string)           { f.likeCalls <- reactionType }
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

func expectLikeCallWithType(t *testing.T, ch chan string, wantType string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != wantType {
			t.Errorf("fed DeliverLike called with type %q, want %q", got, wantType)
		}
	case <-time.After(300 * time.Millisecond):
		t.Errorf("expected a fed DeliverLike call with type %q, none arrived", wantType)
	}
}

// TestReactPostFederationCallSite covers ReactPost's own dispatch logic —
// what it calls fed/atproto with, not what federation.Service.DeliverLike
// then decides to actually send (that's AGORA-360's isAgoraInstance/valence
// split, covered in internal/federation's own tests).
//
// AGORA-359 first established: ActivityPub/Bluesky only have a plain Like,
// so a fresh reaction only reaches atproto (and, pre-AGORA-360, fed) when
// it's positive/neutral, and switching away from one must retract it.
//
// AGORA-360 changed the fed side specifically: since only
// federation.Service.DeliverLike knows whether the target is a confirmed
// Agora peer (which gets the exact reaction, any valence), ReactPost now
// calls fed.DeliverLike with the new type on every genuine change, full
// stop, and leaves the "what does this actually turn into" decision to it.
// The atproto side is unaffected by AGORA-360 (Bluesky is never "another
// Agora instance") and keeps AGORA-359's plain valence gate exactly.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestReactPostFederationCallSite(t *testing.T) {
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

	t.Run("fresh reaction with a federating type calls fed and atproto Like", func(t *testing.T) {
		userID, postID := newUserAndPost(t)
		fed, atp := newFakeFedSender(), newFakeAtprotoSender()
		s := &Service{db: db, fed: fed, atproto: atp}

		react(t, s, userID, postID, "pride")

		expectLikeCallWithType(t, fed.likeCalls, "pride")
		expectCall(t, atp.likeCalls, true, "atproto DeliverLike")
		expectCall(t, fed.unlikeCalls, false, "fed DeliverUnlike")
	})

	t.Run("fresh reaction with a negative type still reaches fed, but not atproto", func(t *testing.T) {
		userID, postID := newUserAndPost(t)
		fed, atp := newFakeFedSender(), newFakeAtprotoSender()
		s := &Service{db: db, fed: fed, atproto: atp}

		react(t, s, userID, postID, "sad")

		// AGORA-360: fed always hears about a genuine change so it can
		// decide (an Agora peer gets "sad" precisely; anyone else gets
		// nothing) — only atproto's plain valence gate suppresses the call
		// entirely for a negative reaction, since Bluesky has no equivalent
		// of an Agora peer to send the exact type to.
		expectLikeCallWithType(t, fed.likeCalls, "sad")
		expectCall(t, atp.likeCalls, false, "atproto DeliverLike")
	})

	t.Run("re-selecting the same reaction calls neither", func(t *testing.T) {
		userID, postID := newUserAndPost(t)
		fed, atp := newFakeFedSender(), newFakeAtprotoSender()
		s := &Service{db: db, fed: fed, atproto: atp}

		react(t, s, userID, postID, "love")
		expectLikeCallWithType(t, fed.likeCalls, "love")
		expectCall(t, atp.likeCalls, true, "atproto DeliverLike")

		react(t, s, userID, postID, "love")
		expectCall(t, fed.likeCalls, false, "fed DeliverLike (re-send of an unchanged reaction)")
		expectCall(t, atp.likeCalls, false, "atproto DeliverLike (re-send of an unchanged reaction)")
	})

	t.Run("switching between two federating reactions notifies fed with the new type, not atproto", func(t *testing.T) {
		userID, postID := newUserAndPost(t)
		fed, atp := newFakeFedSender(), newFakeAtprotoSender()
		s := &Service{db: db, fed: fed, atproto: atp}

		react(t, s, userID, postID, "love")
		expectLikeCallWithType(t, fed.likeCalls, "love")
		expectCall(t, atp.likeCalls, true, "atproto DeliverLike")

		react(t, s, userID, postID, "pride")
		expectLikeCallWithType(t, fed.likeCalls, "pride")
		expectCall(t, fed.unlikeCalls, false, "fed DeliverUnlike")
		expectCall(t, atp.likeCalls, false, "atproto DeliverLike (re-send, already liked)")
		expectCall(t, atp.unlikeCalls, false, "atproto DeliverUnlike")
	})

	t.Run("switching from a federating to a negative reaction: fed gets the new type, atproto retracts", func(t *testing.T) {
		userID, postID := newUserAndPost(t)
		fed, atp := newFakeFedSender(), newFakeAtprotoSender()
		s := &Service{db: db, fed: fed, atproto: atp}

		react(t, s, userID, postID, "like")
		expectLikeCallWithType(t, fed.likeCalls, "like")
		expectCall(t, atp.likeCalls, true, "atproto DeliverLike")

		react(t, s, userID, postID, "angry")
		expectLikeCallWithType(t, fed.likeCalls, "angry")
		expectCall(t, atp.unlikeCalls, true, "atproto DeliverUnlike")
	})

	t.Run("switching between two negative reactions still notifies fed, not atproto", func(t *testing.T) {
		userID, postID := newUserAndPost(t)
		fed, atp := newFakeFedSender(), newFakeAtprotoSender()
		s := &Service{db: db, fed: fed, atproto: atp}

		react(t, s, userID, postID, "sad")
		expectLikeCallWithType(t, fed.likeCalls, "sad")

		react(t, s, userID, postID, "angry")
		expectLikeCallWithType(t, fed.likeCalls, "angry")

		expectCall(t, atp.likeCalls, false, "atproto DeliverLike")
		expectCall(t, atp.unlikeCalls, false, "atproto DeliverUnlike")
	})

	t.Run("a prior legacy plain like row is treated as a federated like for atproto's gate", func(t *testing.T) {
		userID, postID := newUserAndPost(t)
		fed, atp := newFakeFedSender(), newFakeAtprotoSender()
		s := &Service{db: db, fed: fed, atproto: atp}

		if _, err := db.Exec(`INSERT INTO likes (user_id, post_id) VALUES ($1, $2)`, userID, postID); err != nil {
			t.Fatalf("insert legacy like row: %v", err)
		}

		react(t, s, userID, postID, "angry")
		expectLikeCallWithType(t, fed.likeCalls, "angry")
		expectCall(t, atp.unlikeCalls, true, "atproto DeliverUnlike")
	})
}
