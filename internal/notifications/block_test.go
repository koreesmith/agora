package notifications

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/agora-social/agora/internal/ctxkeys"
)

// AGORA-426: a notification from an account on either side of a block isn't
// listed or counted, and a new one isn't created at all.
func TestBlockedActorsNotifications(t *testing.T) {
	db := pushTestDB(t)
	s := &Service{db: db}
	me, blocked, blockedMe, friend := pushTestUser(t, db, "me"), pushTestUser(t, db, "blocked"), pushTestUser(t, db, "blockedme"), pushTestUser(t, db, "friend")
	for _, actor := range []string{blocked, blockedMe, friend} {
		db.Exec(`INSERT INTO notifications (user_id, actor_id, type) VALUES ($1, $2, 'atproto_follow')`, me, actor)
	}
	db.Exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1,$2)`, me, blocked)
	db.Exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1,$2)`, blockedMe, me)

	if got := s.unreadCount(me); got != 1 {
		t.Errorf("unreadCount = %d, want 1 (only the friend's)", got)
	}

	ctx := context.WithValue(context.Background(), ctxkeys.UserID, me)
	w := httptest.NewRecorder()
	s.List(w, httptest.NewRequest("GET", "/notifications", nil).WithContext(ctx))
	var body struct {
		Notifications []struct {
			ActorID *string `json:"actor_id"`
		} `json:"notifications"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Notifications) != 1 || body.Notifications[0].ActorID == nil || *body.Notifications[0].ActorID != friend {
		t.Errorf("listed %d notifications, want only the friend's", len(body.Notifications))
	}

	s.Create(me, blocked, "post_mention", "", "")
	s.Create(me, blockedMe, "post_mention", "", "")
	var created int
	db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND type = 'post_mention'`, me).Scan(&created)
	if created != 0 {
		t.Errorf("Create made %d notifications across a block, want 0", created)
	}
}
