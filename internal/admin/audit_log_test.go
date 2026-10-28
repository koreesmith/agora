package admin

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-397: the audit log returned its latest 200 rows and ignored page, so
// a client's Previous/Next did nothing, and a user-targeted entry carried only
// the target's UUID. It now pages with has_more and names the target account.
//
// The rows are dated a day ahead so they are the newest in the shared test
// database whatever else is in it.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestGetAuditLogPagesAndNamesTargets(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil { t.Skipf("skipping: agora-postgres-test not reachable: %v", err) }
	// A Cleanup, not a defer: deferred calls run before t.Cleanup functions,
	// which would leave every cleanup below hitting a closed pool. Registered
	// first, so it runs last.
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil { t.Fatalf("migrate: %v", err) }

	u := time.Now().UnixNano()
	mkUser := func(tag string) string {
		var id string
		if err := db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`,
			fmt.Sprintf("al_%s_%d", tag, u)).Scan(&id); err != nil {
			t.Fatalf("seed user: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id=$1`, id) })
		return id
	}
	admin, target := mkUser("adm"), mkUser("tgt")

	// Newest first: set_role on target, then an instance action, then settings.
	var ids []string
	for i, row := range []struct{ action, ttype, tid string }{
		{"set_role", "user", target},
		{"block_instance", "instance", "spam.example"},
		{"update_settings", "settings", ""},
	} {
		var id string
		if err := db.QueryRow(`INSERT INTO audit_log (actor_id, action, target_type, target_id, details, created_at)
			VALUES ($1,$2,$3,$4,'d', NOW() + interval '1 day' - make_interval(secs => $5)) RETURNING id`,
			admin, row.action, row.ttype, row.tid, i).Scan(&id); err != nil {
			t.Fatalf("seed audit row: %v", err)
		}
		ids = append(ids, id)
	}
	t.Cleanup(func() {
		for _, id := range ids { db.Exec(`DELETE FROM audit_log WHERE id=$1`, id) }
	})

	old := auditPageSize
	auditPageSize = 2
	t.Cleanup(func() { auditPageSize = old })

	s := &Service{db: db}
	type entry struct {
		ID             string  `json:"id"`
		Action         string  `json:"action"`
		TargetUsername *string `json:"target_username"`
		Actor          *string `json:"actor_username"`
	}
	get := func(page string) ([]entry, bool) {
		t.Helper()
		w := httptest.NewRecorder()
		s.GetAuditLog(w, httptest.NewRequest("GET", "/admin/audit-log?page="+page, nil))
		if w.Code != 200 { t.Fatalf("page %s: status %d: %s", page, w.Code, w.Body.String()) }
		var body struct {
			Entries []entry `json:"entries"`
			HasMore bool    `json:"has_more"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil { t.Fatalf("decode: %v", err) }
		return body.Entries, body.HasMore
	}

	first, more := get("0")
	if len(first) != 2 || first[0].ID != ids[0] || first[1].ID != ids[1] {
		t.Fatalf("page 0 = %+v, want the two newest seeded rows", first)
	}
	if !more { t.Error("page 0 has_more = false, want true") }
	if first[0].TargetUsername == nil || *first[0].TargetUsername != fmt.Sprintf("al_tgt_%d", u) {
		t.Errorf("set_role target_username = %v, want the target's username", first[0].TargetUsername)
	}
	if first[1].TargetUsername != nil {
		t.Errorf("instance action target_username = %q, want none", *first[1].TargetUsername)
	}
	if first[0].Actor == nil || *first[0].Actor != fmt.Sprintf("al_adm_%d", u) {
		t.Errorf("actor_username = %v, want the admin's username", first[0].Actor)
	}

	second, _ := get("1")
	if len(second) == 0 || second[0].ID != ids[2] {
		t.Fatalf("page 1 starts with %+v, want the third seeded row", second)
	}

	if neg, _ := get("-3"); len(neg) == 0 || neg[0].ID != ids[0] {
		t.Errorf("a negative page should read as page 0")
	}
}
