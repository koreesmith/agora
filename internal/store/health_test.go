package store

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"
)

// AGORA-458

func openHealthTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	return db
}

func TestHealthReportsThePool(t *testing.T) {
	db := openHealthTestDB(t)
	defer db.Close()
	h := db.Health(2 * time.Second)
	if !h.OK || h.Error != "" {
		t.Fatalf("health = %+v, want ok", h)
	}
	if h.MaxOpen != maxOpenConns {
		t.Errorf("max_open = %d, want %d", h.MaxOpen, maxOpenConns)
	}
}

// A database that does not answer is reported, within the limit, not waited on.
func TestHealthReportsAStarvedPool(t *testing.T) {
	db := openHealthTestDB(t)
	defer db.Close()
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(context.Background()) // takes the only connection and keeps it
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	start := time.Now()
	h := db.Health(300 * time.Millisecond)
	if h.OK {
		t.Fatal("health ok while every connection is held")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("health waited %v, want about its 300ms limit", time.Since(start))
	}
	if h.InUse != 1 || h.Error == "" {
		t.Errorf("health = %+v, want one connection in use and an error", h)
	}
}

func TestWatchdogExitsAfterConsecutiveFailures(t *testing.T) {
	db := openHealthTestDB(t)
	db.Close() // every ping now fails

	var code atomic.Int32
	done := make(chan struct{})
	go func() {
		db.Watchdog(context.Background(), 3, 10*time.Millisecond, func(c int) { code.Store(int32(c)) })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not give up on a dead database")
	}
	if code.Load() != 1 {
		t.Errorf("exit code = %d, want 1", code.Load())
	}
}

func TestWatchdogStaysQuietWhileHealthy(t *testing.T) {
	db := openHealthTestDB(t)
	defer db.Close()
	var exited atomic.Bool
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	db.Watchdog(ctx, 2, 20*time.Millisecond, func(int) { exited.Store(true) })
	if exited.Load() {
		t.Fatal("watchdog exited a healthy server")
	}
}

func TestWatchdogOffByDefault(t *testing.T) {
	var db DB
	db.Watchdog(context.Background(), 0, time.Millisecond, func(int) { t.Fatal("exit called") }) // returns at once
}

func TestPoolPressure(t *testing.T) {
	calm := sql.DBStats{InUse: 3, WaitCount: 5}
	if hot, _ := poolPressure(calm, calm); hot {
		t.Error("a quiet pool is not under pressure")
	}
	if hot, _ := poolPressure(calm, sql.DBStats{InUse: 20, WaitCount: 5}); !hot {
		t.Error("20 of 25 in use is pressure")
	}
	if hot, _ := poolPressure(calm, sql.DBStats{InUse: 3, WaitCount: 9}); !hot {
		t.Error("requests that queued for a connection is pressure")
	}
}
