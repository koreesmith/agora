package dm

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

type recordedPush struct {
	recipient string
	request   bool
}

type fakePusher struct {
	mu     sync.Mutex
	pushes []recordedPush
}

func (f *fakePusher) PushDirectMessage(recipientID, _, _ string, isRequest bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pushes = append(f.pushes, recordedPush{recipientID, isRequest})
}
func (f *fakePusher) SyncBadge(string) {}

// AGORA-425: a new message pushes to every local recipient, says so when it's
// a request the recipient hasn't accepted, and never pushes to the sender or
// to a remote participant, whose own instance has their devices.
func TestPushNewMessageRecipients(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	unique := time.Now().UnixNano()
	mkUser := func(tag string, remote bool) string {
		var id string
		if err := db.QueryRow(`INSERT INTO users (username,email,password_hash,is_remote) VALUES ($1,$1,'x',$2) RETURNING id`,
			fmt.Sprintf("dm425_%s_%d", tag, unique), remote).Scan(&id); err != nil {
			t.Fatalf("seeding %s: %v", tag, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	sender, friend, stranger, remote := mkUser("sender", false), mkUser("friend", false), mkUser("stranger", false), mkUser("remote", true)

	var convID string
	db.QueryRow(`INSERT INTO conversations DEFAULT VALUES RETURNING id`).Scan(&convID)
	t.Cleanup(func() { db.Exec(`DELETE FROM conversations WHERE id = $1`, convID) })
	for _, p := range []struct {
		id       string
		accepted bool
	}{{sender, true}, {friend, true}, {stranger, false}, {remote, true}} {
		db.Exec(`INSERT INTO conversation_participants (conversation_id, user_id, is_accepted) VALUES ($1,$2,$3)`, convID, p.id, p.accepted)
	}

	f := &fakePusher{}
	s := &Service{db: db, push: f}
	s.pushNewMessage(convID, sender)

	sort.Slice(f.pushes, func(i, j int) bool { return f.pushes[i].recipient < f.pushes[j].recipient })
	want := []recordedPush{{friend, false}, {stranger, true}}
	sort.Slice(want, func(i, j int) bool { return want[i].recipient < want[j].recipient })
	if fmt.Sprint(f.pushes) != fmt.Sprint(want) {
		t.Errorf("pushes = %v, want %v", f.pushes, want)
	}
}
