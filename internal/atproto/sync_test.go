package atproto

import (
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// testDB opens a connection to the local isolated test database (never the
// production one — see feedback_isolated_test_db), skipping the test rather
// than failing it if that database isn't reachable, since this package's
// other tests are all pure/no-DB and shouldn't require test-DB setup to run.
func testDB(t *testing.T) *store.DB {
	t.Helper()
	dsn := "postgres://agora:agora@localhost:15433/agora_test?sslmode=disable"
	db, err := store.Open(dsn)
	if err != nil {
		t.Skipf("test DB not reachable at %s, skipping: %v", dsn, err)
	}
	return db
}

// TestListReposNoCursor is a regression test for AGORA-240: SQL doesn't
// short-circuit OR, so "($1 = '' OR id > $1)" used to type-check id > $1
// against a uuid column even on the empty-cursor (first-page) branch,
// 500'ing on literally every call the relay makes without a prior cursor —
// which is its normal case. Exercises the real HTTP handler end-to-end
// (not just the query) since that's the layer the bug actually surfaced at.
func TestListReposNoCursor(t *testing.T) {
	db := testDB(t)

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "http://localhost:8080"}}

	// Save/restore the instance-wide toggle rather than leaving it forced
	// on — this test DB is shared with manual verification sessions.
	var prevEnabled string
	db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'atproto_enabled'`).Scan(&prevEnabled)
	db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('atproto_enabled', 'true') ON CONFLICT (key) DO UPDATE SET value = 'true'`)
	t.Cleanup(func() {
		db.Exec(`UPDATE instance_settings SET value = $1 WHERE key = 'atproto_enabled'`, prevEnabled)
	})

	const username = "agora240_regress_test_user"
	const email = "agora240_regress_test_user@example.invalid"
	const did = "did:web:agora240_regress_test_user"
	db.Exec(`DELETE FROM users WHERE username = $1`, username)
	if _, err := db.Exec(`
		INSERT INTO users (username, email, password_hash, profile_private, atproto_enabled, atproto_did, atproto_repo_head, atproto_repo_rev)
		VALUES ($1, $2, '', false, true, $3, 'bafyfakehead', 'fakerev')
	`, username, email, did); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE username = $1`, username) })

	r := chi.NewRouter()
	r.Get("/xrpc/com.atproto.sync.listRepos", s.ListRepos)

	// The exact shape of the relay's real call that 500'd in production:
	// limit set, no cursor at all.
	req := httptest.NewRequest("GET", "/xrpc/com.atproto.sync.listRepos?limit=1000", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("no-cursor call: status = %d, body = %s (want 200)", rec.Code, rec.Body.String())
	}

	// Cursor-continuation path should still work correctly too.
	req2 := httptest.NewRequest("GET", "/xrpc/com.atproto.sync.listRepos?limit=1000&cursor=00000000-0000-0000-0000-000000000000", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)

	if rec2.Code != 200 {
		t.Fatalf("cursor call: status = %d, body = %s (want 200)", rec2.Code, rec2.Body.String())
	}
}
