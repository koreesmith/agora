package store

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// AGORA-458: the server holds at most maxOpenConns database connections and a
// query waits for ever for a free one, so if they are all held by something
// stuck, every request hangs and nothing says why. These make that visible.

const maxOpenConns = 25

// Health pings the database with a short limit and returns the pool's numbers.
type Health struct {
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
	PingMS      int64  `json:"ping_ms"`
	Open        int    `json:"open"`
	InUse       int    `json:"in_use"`
	Idle        int    `json:"idle"`
	MaxOpen     int    `json:"max_open"`
	WaitCount   int64  `json:"wait_count"`   // requests that had to wait for a connection, since start
	WaitSeconds int64  `json:"wait_seconds"` // total time spent waiting, since start
}

func (db *DB) Health(timeout time.Duration) Health {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	start := time.Now()
	err := db.PingContext(ctx)
	st := db.Stats()
	h := Health{
		OK: err == nil, PingMS: time.Since(start).Milliseconds(),
		Open: st.OpenConnections, InUse: st.InUse, Idle: st.Idle, MaxOpen: st.MaxOpenConnections,
		WaitCount: st.WaitCount, WaitSeconds: int64(st.WaitDuration.Seconds()),
	}
	if err != nil {
		h.Error = err.Error()
	}
	return h
}

// poolPressure reports whether the pool looks starved: nearly every
// connection in use, or requests that had to queue for one since last looked.
func poolPressure(prev, cur sql.DBStats) (bool, string) {
	queued := cur.WaitCount - prev.WaitCount
	if cur.InUse >= maxOpenConns*8/10 || queued > 0 {
		return true, ""
	}
	return false, ""
}

// MonitorPool logs a line whenever the pool is nearly full or requests queued
// for a connection since the last check, at most once a minute, so a freeze
// leaves its start in the log. It runs until ctx ends.
func (db *DB) MonitorPool(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	prev := db.Stats()
	var lastLogged time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		cur := db.Stats()
		if hot, _ := poolPressure(prev, cur); hot && time.Since(lastLogged) >= time.Minute {
			log.Printf("db pool under pressure: %d of %d connections in use, %d idle, %d requests waited for a connection in the last 10s (%.1fs waiting in total since start)",
				cur.InUse, cur.MaxOpenConnections, cur.Idle, cur.WaitCount-prev.WaitCount, cur.WaitDuration.Seconds())
			lastLogged = time.Now()
		}
		prev = cur
	}
}

// Watchdog exits the process after `failures` database checks in a row have
// failed, so a restart policy (docker compose's restart: unless-stopped)
// brings it back without anyone running docker compose restart. Zero or less
// disables it. exit is a parameter so tests do not end the test binary.
func (db *DB) Watchdog(ctx context.Context, failures int, interval time.Duration, exit func(code int)) {
	if failures <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	bad := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		h := db.Health(5 * time.Second)
		if h.OK {
			bad = 0
			continue
		}
		bad++
		log.Printf("watchdog: database check failed (%d of %d): %s; pool %d of %d in use, %d waited", bad, failures, h.Error, h.InUse, h.MaxOpen, h.WaitCount)
		if bad >= failures {
			log.Printf("watchdog: the database has not answered %d checks in a row; exiting so the container restarts", failures)
			exit(1)
			return
		}
	}
}
