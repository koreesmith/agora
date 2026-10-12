package federation

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-353: drainAPQueue used to have no row locking, and apQueueTicker
// fires a new goroutine every 20s regardless of whether the previous one
// finished — so an overrunning batch (several slow/dead inboxes, each held
// up to fedHTTPClient's 10s timeout) could have its still-in-flight rows
// picked up a second time by the next tick, double-processing them and
// doubling the DB connections held blocked on outbound HTTP. This exercises
// claimAPDeliveryJobs' exclusivity guarantee directly against a real DB
// (network delivery itself isn't reachable in tests — fedHTTPClient refuses
// to dial loopback by design).
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestClaimAPDeliveryJobsIsExclusive(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}

	// This is the isolated agora-postgres-test instance, never production —
	// clearing the queue first keeps the exclusivity assertions below from
	// depending on whatever due rows other tests/sessions may have left
	// behind (claimAPDeliveryJobs' LIMIT 20 could otherwise fill up on
	// unrelated leftover rows before ever reaching this test's own 12).
	db.Exec(`DELETE FROM ap_delivery_queue`)

	var userID string
	username := fmt.Sprintf("agora353_%d", time.Now().UnixNano())
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')
		RETURNING id
	`, username, username+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	const numRows = 12
	var ids []string
	for i := 0; i < numRows; i++ {
		var id string
		if err := db.QueryRow(`
			INSERT INTO ap_delivery_queue (actor_user_id, inbox_url, activity, next_attempt)
			VALUES ($1, $2, '{}', NOW())
			RETURNING id
		`, userID, fmt.Sprintf("https://example.test/inbox/%d", i)).Scan(&id); err != nil {
			t.Fatalf("insert queue row: %v", err)
		}
		ids = append(ids, id)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM ap_delivery_queue WHERE actor_user_id = $1`, userID) })

	mine := make(map[string]bool, len(ids))
	for _, id := range ids {
		mine[id] = true
	}

	// Two concurrent "drain ticks" racing for the same due rows, exactly the
	// scenario an overrunning drainAPQueue plus the 20s ticker could produce.
	// The shared test DB may hold other due rows left by other tests/sessions
	// (LIMIT 20 per claim, so a busy table could even split our 12 across
	// both callers) — only this test's own ids are asserted on below.
	const numClaimers = 2
	results := make([][]apDeliveryJob, numClaimers)
	var wg sync.WaitGroup
	for i := 0; i < numClaimers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			jobs, err := s.claimAPDeliveryJobs()
			if err != nil {
				t.Errorf("claimAPDeliveryJobs: %v", err)
				return
			}
			results[i] = jobs
		}(i)
	}
	wg.Wait()

	seen := make(map[string]int)
	for _, jobs := range results {
		for _, j := range jobs {
			if mine[j.id] {
				seen[j.id]++
			}
		}
	}
	if len(seen) != numRows {
		t.Errorf("claimed %d of this test's %d rows across %d concurrent claimers, want all %d claimed exactly once (LIMIT 20 per claim may need a 2nd claim round for a busier table)",
			len(seen), numRows, numClaimers, numRows)
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("row %s was claimed by %d concurrent callers, want exactly 1 (FOR UPDATE SKIP LOCKED should have prevented this)", id, count)
		}
	}
}

// AGORA-353: a row that exhausts maxDeliveryAttempts must be marked dead_at
// so it's queryable as abandoned, and the backoff between any two attempts
// must never exceed deliveryBackoffCapMinutes (previously capped at 1440 —
// 24h — which combined with maxDeliveryAttempts' doubling schedule meant a
// wait of 2-8.5 hours on a delivery's 7th-9th attempt).
func TestDeliveryBackoffCapAndDeadAt(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var userID string
	username := fmt.Sprintf("agora353b_%d", time.Now().UnixNano())
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')
		RETURNING id
	`, username, username+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	var id string
	if err := db.QueryRow(`
		INSERT INTO ap_delivery_queue (actor_user_id, inbox_url, activity, next_attempt, attempts)
		VALUES ($1, 'https://example.test/inbox/dead', '{}', NOW(), $2)
		RETURNING id
	`, userID, maxDeliveryAttempts-1).Scan(&id); err != nil {
		t.Fatalf("insert queue row: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM ap_delivery_queue WHERE id = $1`, id) })

	// Simulate drainAPQueue's own failure-path UPDATE for a row on its final
	// allowed attempt (attempts = maxDeliveryAttempts-1 going in).
	givingUp := true
	if _, err := db.Exec(`
		UPDATE ap_delivery_queue
		SET attempts = attempts + 1,
		    last_error = 'simulated failure',
		    next_attempt = NOW() + (LEAST(POWER(2, attempts), $1) * INTERVAL '1 minute'),
		    dead_at = CASE WHEN $2 THEN NOW() ELSE dead_at END
		WHERE id = $3
	`, deliveryBackoffCapMinutes, givingUp, id); err != nil {
		t.Fatalf("simulate failure update: %v", err)
	}

	var attempts int
	var deadAt *time.Time
	var nextAttempt time.Time
	if err := db.QueryRow(`SELECT attempts, dead_at, next_attempt FROM ap_delivery_queue WHERE id = $1`, id).
		Scan(&attempts, &deadAt, &nextAttempt); err != nil {
		t.Fatalf("read back row: %v", err)
	}

	if attempts != maxDeliveryAttempts {
		t.Errorf("attempts = %d, want %d", attempts, maxDeliveryAttempts)
	}
	if deadAt == nil {
		t.Error("dead_at was not set after exhausting maxDeliveryAttempts")
	}
	if wait := time.Until(nextAttempt); wait > time.Duration(deliveryBackoffCapMinutes)*time.Minute+time.Minute {
		t.Errorf("next_attempt is %v out, want at most ~%d minutes (deliveryBackoffCapMinutes)", wait, deliveryBackoffCapMinutes)
	}
}
