package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
)

// AGORA-323: an inbound direct message meets the same policy a local one does.
//
// That equivalence is the design, and it is also where getting it wrong is a
// privacy failure rather than a bug: dm_privacy is a user's own answer to "who
// can message me", and an answer that only holds for local senders is not an
// answer. The cases below are the three settings plus a block.
func TestInboundDirectMessagePolicy(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}
	unique := time.Now().UnixNano()
	peer := fmt.Sprintf("dmpeer323-%d.example", unique)
	senderActor := "https://" + peer + "/federation/users/bob"

	var senderID string
	if err := db.QueryRow(`
		INSERT INTO users (username,email,password_hash,display_name,is_remote,remote_instance,ap_actor_url)
		VALUES ($1,$1,'x','Bob',true,$2,$3) RETURNING id
	`, fmt.Sprintf("dm323_s_%d", unique), peer, senderActor).Scan(&senderID); err != nil {
		t.Fatalf("seeding the sender failed: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, senderID) })

	// One local recipient per case, so the cases cannot contaminate each other
	// through a conversation left behind by an earlier one.
	mkRecipient := func(tag, privacy string) (string, string) {
		name := fmt.Sprintf("dm323_%s_%d", tag, unique)
		var id string
		if err := db.QueryRow(`
			INSERT INTO users (username,email,password_hash,dm_privacy) VALUES ($1,$1,'x',$2) RETURNING id
		`, name, privacy).Scan(&id); err != nil {
			t.Fatalf("seeding recipient %s failed: %v", tag, err)
		}
		t.Cleanup(func() {
			db.Exec(`DELETE FROM conversations WHERE id IN (SELECT conversation_id FROM conversation_participants WHERE user_id = $1)`, id)
			db.Exec(`DELETE FROM users WHERE id = $1`, id)
		})
		return id, name
	}

	deliver := func(recipientName, msgID string) {
		s.handleInboundDirectMessage(senderActor, inboundNote{
			ID:            msgID,
			Content:       "hello",
			Published:     time.Now().UTC().Format(time.RFC3339),
			To:            []string{"https://local.example/federation/users/" + recipientName},
			DirectMessage: true,
		})
	}
	stored := func(recipientID string) int {
		var n int
		db.QueryRow(`
			SELECT COUNT(*) FROM messages m
			JOIN conversation_participants cp ON cp.conversation_id = m.conversation_id
			WHERE cp.user_id = $1 AND m.author_id = $2
		`, recipientID, senderID).Scan(&n)
		return n
	}

	t.Run("everyone accepts a stranger's message", func(t *testing.T) {
		id, name := mkRecipient("everyone", "everyone")
		deliver(name, fmt.Sprintf("https://%s/msg/a-%d", peer, unique))
		if stored(id) != 1 {
			t.Error("a message to a user who accepts messages from everyone was not stored")
		}
	})

	t.Run("nobody refuses it", func(t *testing.T) {
		id, name := mkRecipient("nobody", "nobody")
		deliver(name, fmt.Sprintf("https://%s/msg/b-%d", peer, unique))
		if stored(id) != 0 {
			t.Error("a user who accepts no messages received one from another instance")
		}
	})

	t.Run("friends only refuses a non-friend", func(t *testing.T) {
		id, name := mkRecipient("friendsonly", "friends")
		deliver(name, fmt.Sprintf("https://%s/msg/c-%d", peer, unique))
		if stored(id) != 0 {
			t.Error("a user who accepts messages from friends only received one from a stranger on another instance")
		}
	})

	t.Run("friends only accepts a friend", func(t *testing.T) {
		id, name := mkRecipient("friendok", "friends")
		if _, err := db.Exec(`INSERT INTO friendships (requester_id,addressee_id,status) VALUES ($1,$2,'accepted')`, id, senderID); err != nil {
			t.Fatalf("seeding the friendship failed: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM friendships WHERE requester_id = $1`, id) })
		deliver(name, fmt.Sprintf("https://%s/msg/d-%d", peer, unique))
		if stored(id) != 1 {
			t.Error("a friend's message was refused")
		}
	})

	t.Run("a block refuses it whatever the setting", func(t *testing.T) {
		id, name := mkRecipient("blocked", "everyone")
		if _, err := db.Exec(`INSERT INTO blocks (blocker_id,blocked_id) VALUES ($1,$2)`, id, senderID); err != nil {
			t.Fatalf("seeding the block failed: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM blocks WHERE blocker_id = $1`, id) })
		deliver(name, fmt.Sprintf("https://%s/msg/e-%d", peer, unique))
		if stored(id) != 0 {
			t.Error("a blocked sender's message was stored")
		}
	})

	t.Run("redelivery does not duplicate", func(t *testing.T) {
		id, name := mkRecipient("dedupe", "everyone")
		msgID := fmt.Sprintf("https://%s/msg/f-%d", peer, unique)
		deliver(name, msgID)
		deliver(name, msgID)
		deliver(name, msgID)
		if n := stored(id); n != 1 {
			t.Errorf("stored %d copies, want 1. Peers retry on their own schedule, and a conversation is the worst place to see a message twice.", n)
		}
	})

	t.Run("a stranger's message lands as a request, not in the inbox", func(t *testing.T) {
		id, name := mkRecipient("request", "everyone")
		deliver(name, fmt.Sprintf("https://%s/msg/g-%d", peer, unique))
		var accepted bool
		db.QueryRow(`
			SELECT cp.is_accepted FROM conversation_participants cp
			JOIN messages m ON m.conversation_id = cp.conversation_id
			WHERE cp.user_id = $1 LIMIT 1
		`, id).Scan(&accepted)
		if accepted {
			t.Error("a stranger's first message from another instance skipped the request state a local one would land in")
		}
	})
}

// TestInboundDirectMessageRecognition covers what counts as a direct message at
// all. Reading this wrong in either direction is bad: a post treated as a
// message vanishes from the feed into somebody's inbox, and a message treated
// as a post is published.
func TestInboundDirectMessageRecognition(t *testing.T) {
	db := testFriendshipService(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://local.example"}}

	cases := []struct {
		name string
		note inboundNote
		want bool
	}{
		{"marked and addressed to one actor", inboundNote{To: []string{"https://local.example/federation/users/nobody"}, DirectMessage: true}, true},
		// Mastodon sends no marker; a Note to exactly one actor with no
		// inReplyTo is its direct message, and reading it that way is what
		// lets a message from Mastodon arrive as a message.
		{"unmarked but addressed to exactly one actor", inboundNote{To: []string{"https://local.example/federation/users/nobody"}}, true},
		{"a reply is never a direct message", inboundNote{To: []string{"https://local.example/federation/users/nobody"}, InReplyTo: "https://x/1", DirectMessage: true}, false},
		{"a public note is never a direct message", inboundNote{To: []string{"https://www.w3.org/ns/activitystreams#Public"}, DirectMessage: true}, false},
		{"unmarked and addressed to several is a post", inboundNote{To: []string{"https://a/1", "https://a/2"}}, false},
		{"unaddressed is a post", inboundNote{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := s.handleInboundDirectMessage("https://peer.example/federation/users/nobody", c.note); got != c.want {
				t.Errorf("treated as a direct message = %v, want %v", got, c.want)
			}
		})
	}
}
