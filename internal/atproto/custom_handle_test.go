package atproto

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// seedCustomDomainUser creates a local account plus a custom_domains row in
// the given state, and returns the username and domain.
func seedCustomDomainUser(t *testing.T, db *store.DB, prefix, verification, approval string) (username, domain string) {
	t.Helper()
	unique := time.Now().UnixNano()
	username = fmt.Sprintf("%s_%d", prefix, unique)
	domain = fmt.Sprintf("byo-%d.example", unique)

	var userID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, profile_private, atproto_enabled, atproto_did, atproto_private_key)
		VALUES ($1, $2, '', false, true, $3, '')
		RETURNING id
	`, username, username+"@example.invalid", "did:web:"+username+".agora.example").Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	if _, err := db.Exec(`
		INSERT INTO custom_domains (user_id, domain, protocol, verification_method, verification_status, approval_status, verified_at)
		VALUES ($1, $2, 'atproto', 'dns', $3, $4, NOW())
	`, userID, domain, verification, approval); err != nil {
		t.Fatalf("seed custom domain: %v", err)
	}
	return username, domain
}

func customHandleService(db *store.DB) *Service {
	return &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora.example"}}
}

func enableATProto(t *testing.T, db *store.DB) {
	t.Helper()
	var prev string
	db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'atproto_enabled'`).Scan(&prev)
	db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('atproto_enabled', 'true')
		ON CONFLICT (key) DO UPDATE SET value = 'true'`)
	t.Cleanup(func() { db.Exec(`UPDATE instance_settings SET value = $1 WHERE key = 'atproto_enabled'`, prev) })
}

func fetchDIDDoc(t *testing.T, s *Service, host string) (map[string]any, int) {
	t.Helper()
	req := httptest.NewRequest("GET", "/.well-known/did.json", nil)
	req.Host = host
	w := httptest.NewRecorder()
	s.DIDDocument(w, req)

	var doc map[string]any
	json.NewDecoder(w.Body).Decode(&doc)
	return doc, w.Code
}

func alsoKnownAs(t *testing.T, doc map[string]any) []string {
	t.Helper()
	raw, ok := doc["alsoKnownAs"].([]any)
	if !ok {
		t.Fatalf("DID document has no alsoKnownAs array: %v", doc)
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		out = append(out, fmt.Sprint(v))
	}
	return out
}

// TestDIDDocumentPublishesApprovedCustomDomain is AGORA-282: an approved
// domain appears in alsoKnownAs ahead of the instance handle (AT Proto reads
// the first entry as primary), and the instance handle stays published
// alongside it — this is an alias, not a migration, so the DID and the
// original handle both keep working.
func TestDIDDocumentPublishesApprovedCustomDomain(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	enableATProto(t, db)
	s := customHandleService(db)

	username, domain := seedCustomDomainUser(t, db, "agora282_live", "verified", "approved")

	doc, code := fetchDIDDoc(t, s, username+".agora.example")
	if code != 200 {
		t.Fatalf("DID document returned %d, want 200", code)
	}

	aka := alsoKnownAs(t, doc)
	want := []string{"at://" + domain, "at://" + username + ".agora.example"}
	if len(aka) != len(want) {
		t.Fatalf("alsoKnownAs = %v, want %v", aka, want)
	}
	for i := range want {
		if aka[i] != want[i] {
			t.Errorf("alsoKnownAs[%d] = %q, want %q", i, aka[i], want[i])
		}
	}

	// The canonical identity itself must be untouched by the alias.
	if got := doc["id"]; got != "did:web:"+username+".agora.example" {
		t.Errorf("DID changed to %v — a custom handle is an alias, not a migration", got)
	}
}

// TestDIDDocumentWithholdsUnapprovedDomain covers both halves of the "live"
// rule (AGORA-285): a domain that verifies but hasn't been approved is not
// published, and neither is one that was approved but has since stopped
// verifying (AGORA-289's lapsed-domain case, which is what makes the fallback
// to the instance handle automatic rather than a teardown step).
func TestDIDDocumentWithholdsUnapprovedDomain(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	enableATProto(t, db)
	s := customHandleService(db)

	for _, tc := range []struct{ name, verification, approval string }{
		{"verified but awaiting review", "verified", "pending"},
		{"verified but rejected", "verified", "rejected"},
		{"approved but no longer verifying", "failed", "approved"},
		{"never verified", "pending", "pending"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			username, domain := seedCustomDomainUser(t, db, "agora282_held", tc.verification, tc.approval)

			doc, code := fetchDIDDoc(t, s, username+".agora.example")
			if code != 200 {
				t.Fatalf("DID document returned %d, want 200", code)
			}
			aka := alsoKnownAs(t, doc)
			if len(aka) != 1 || aka[0] != "at://"+username+".agora.example" {
				t.Errorf("alsoKnownAs = %v, want only the instance handle", aka)
			}
			for _, h := range aka {
				if h == "at://"+domain {
					t.Errorf("published %s despite verification=%s approval=%s", domain, tc.verification, tc.approval)
				}
			}
		})
	}
}

// TestResolvesCustomDomainHost is AGORA-283: a user who points their own
// domain at this instance (rather than serving the well-known file
// themselves) needs these endpoints to answer for that Host, or the handle
// they just proved they own resolves to nothing.
func TestResolvesCustomDomainHost(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	enableATProto(t, db)
	s := customHandleService(db)

	username, domain := seedCustomDomainUser(t, db, "agora283_host", "verified", "approved")
	wantDID := "did:web:" + username + ".agora.example"

	req := httptest.NewRequest("GET", "/.well-known/atproto-did", nil)
	req.Host = domain
	w := httptest.NewRecorder()
	s.AtprotoDIDText(w, req)

	if w.Code != 200 {
		t.Fatalf("atproto-did for custom domain host returned %d, want 200", w.Code)
	}
	if got := w.Body.String(); got != wantDID {
		t.Errorf("atproto-did = %q, want %q", got, wantDID)
	}

	// The DID document has to answer on that host too — a resolver that
	// reaches the DID via the handle then dereferences the document, and a
	// 404 there breaks the round trip the handle exists to complete.
	doc, code := fetchDIDDoc(t, s, domain)
	if code != 200 {
		t.Fatalf("DID document for custom domain host returned %d, want 200", code)
	}
	if got := doc["id"]; got != wantDID {
		t.Errorf("DID document id = %v, want %v", got, wantDID)
	}
}

// TestCustomDomainHostRequiresLiveClaim keeps host resolution honest: a
// domain nobody has finished claiming must not resolve to its claimant, or
// the approval step would be advisory.
func TestCustomDomainHostRequiresLiveClaim(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	enableATProto(t, db)
	s := customHandleService(db)

	_, domain := seedCustomDomainUser(t, db, "agora283_pending", "verified", "pending")

	req := httptest.NewRequest("GET", "/.well-known/atproto-did", nil)
	req.Host = domain
	w := httptest.NewRecorder()
	s.AtprotoDIDText(w, req)

	if w.Code != 404 {
		t.Errorf("atproto-did for an unapproved custom domain returned %d, want 404", w.Code)
	}
}
