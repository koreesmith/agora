package users

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-420: one account, several devices; a device belongs to the account it
// last registered with; and the old empty-token clear still stops everything.
func TestSavePushToken(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	unique := time.Now().UnixNano()
	mkUser := func(tag string) string {
		var id string
		if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`,
			fmt.Sprintf("pt420_%s_%d", tag, unique)).Scan(&id); err != nil {
			t.Fatalf("seeding %s: %v", tag, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	tokensOf := func(userID string) map[string]bool {
		out := map[string]bool{}
		rows, _ := db.Query(`SELECT token FROM push_tokens WHERE user_id = $1`, userID)
		defer rows.Close()
		for rows.Next() {
			var tok string
			rows.Scan(&tok)
			out[tok] = true
		}
		return out
	}
	alice, bob := mkUser("alice"), mkUser("bob")
	phone := fmt.Sprintf("ExponentPushToken[phone-%d]", unique)
	tablet := fmt.Sprintf("ExponentPushToken[tablet-%d]", unique)

	savePushToken(db, alice, phone)
	savePushToken(db, alice, tablet)
	if got := tokensOf(alice); !got[phone] || !got[tablet] {
		t.Fatalf("alice's devices = %v, want both phone and tablet", got)
	}

	// The phone switches to bob: it stops being alice's.
	savePushToken(db, bob, phone)
	if got := tokensOf(alice); got[phone] || !got[tablet] {
		t.Errorf("alice's devices after the phone moved = %v, want only the tablet", got)
	}
	if got := tokensOf(bob); !got[phone] {
		t.Errorf("bob's devices = %v, want the phone", got)
	}

	// An old app clearing its token can't say which device, so all of alice's go.
	savePushToken(db, alice, "")
	if got := tokensOf(alice); len(got) != 0 {
		t.Errorf("alice's devices after an empty-token clear = %v, want none", got)
	}
	if got := tokensOf(bob); !got[phone] {
		t.Errorf("bob's phone was removed by alice's clear")
	}
}
