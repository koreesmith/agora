package domains

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/ctxkeys"
	"github.com/agora-social/agora/internal/store"
)

// Requires the local agora-postgres-test instance (localhost:15433); skips
// rather than failing the suite if it isn't reachable.
//
// Closing is registered as the first cleanup so it runs last: t.Cleanup is
// LIFO, and the per-test row deletions registered later would otherwise fire
// against an already-closed pool and silently no-op, leaving seeded rows
// behind in the shared test database.
func testDB(t *testing.T) *store.DB {
	t.Helper()
	dsn := "postgres://agora:agora@localhost:15433/agora_test?sslmode=disable"
	db, err := store.Open(dsn)
	if err != nil {
		t.Skipf("test DB not reachable at %s, skipping: %v", dsn, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testService(db *store.DB) *Service {
	return &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora.example"}}
}

// seedUser creates a throwaway local account and returns its id and username.
func seedUser(t *testing.T, db *store.DB, prefix string) (string, string) {
	t.Helper()
	username := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	var id string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, profile_private, atproto_enabled, atproto_did)
		VALUES ($1, $2, '', false, true, $3)
		RETURNING id
	`, username, username+"@example.invalid", "did:web:"+username+".agora.example").Scan(&id); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
	return id, username
}

// TestNormalizeDomain covers the input the settings panel actually receives —
// people paste URLs, add an @, and copy values with a trailing dot — alongside
// the two rejections that carry security weight: an IP literal (nobody to
// verify against) and anything inside the instance's own domain, which is
// where the auto-generated per-user handles live and which the instance
// operator, not the claimant, controls the DNS for.
func TestNormalizeDomain(t *testing.T) {
	s := testService(nil)

	valid := map[string]string{
		"example.com":                  "example.com",
		"  Example.COM  ":              "example.com",
		"@example.com":                 "example.com",
		"https://example.com":          "example.com",
		"http://example.com/some/path": "example.com",
		"https://example.com:8443/":    "example.com",
		"example.com.":                 "example.com",
		"my-handle.example.co.uk":      "my-handle.example.co.uk",
	}
	for in, want := range valid {
		got, err := s.normalizeDomain(in)
		if err != nil {
			t.Errorf("normalizeDomain(%q) = error %v, want %q", in, err, want)
			continue
		}
		if got != want {
			t.Errorf("normalizeDomain(%q) = %q, want %q", in, got, want)
		}
	}

	invalid := []string{
		"",
		"   ",
		"localhost",
		"example",
		"exa mple.com",
		"-example.com",
		"example-.com",
		"exam..ple.com",
		"192.168.1.1",
		"agora.example",        // the instance's own domain
		"anyone.agora.example", // and anything under it
	}
	for _, in := range invalid {
		if got, err := s.normalizeDomain(in); err == nil {
			t.Errorf("normalizeDomain(%q) = %q, want an error", in, got)
		}
	}
}

// setInstanceSetting sets an instance_settings value for the duration of the
// test and restores whatever was there before, the same save/restore pattern
// internal/atproto's enableATProto uses. instance_settings is one shared
// table, not something a test gets its own copy of.
func setInstanceSetting(t *testing.T, db *store.DB, key, value string) {
	t.Helper()
	var prev string
	db.QueryRow(`SELECT value FROM instance_settings WHERE key = $1`, key).Scan(&prev)
	db.Exec(`INSERT INTO instance_settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = $2`, key, value)
	t.Cleanup(func() { db.Exec(`UPDATE instance_settings SET value = $1 WHERE key = $2`, prev, key) })
}

// TestSingleUserDomainHandleEnabled covers the AGORA-361 instance-wide switch
// itself: off by default, and reading back whatever an admin sets it to.
func TestSingleUserDomainHandleEnabled(t *testing.T) {
	db := testDB(t)
	s := testService(db)

	setInstanceSetting(t, db, "single_user_domain_handle", "false")
	if s.singleUserDomainHandleEnabled() {
		t.Error("expected the switch to read false")
	}

	setInstanceSetting(t, db, "single_user_domain_handle", "true")
	if !s.singleUserDomainHandleEnabled() {
		t.Error("expected the switch to read true")
	}
}

// TestSoleRealUserID checks the count-based gate against whatever the shared
// test database's real user count actually is, rather than assuming it:
// hundreds of accounts seeded and cleaned up by other tests already live in
// this table, so asserting "exactly one" here would mean either trusting that
// no other test left something behind, or unsafely quarantining every
// existing row for the duration of this one. Comparing the function's answer
// to an independently-computed count is exact either way.
func TestSoleRealUserID(t *testing.T) {
	db := testDB(t)
	s := testService(db)

	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM users WHERE is_remote = false AND deletion_scheduled_at IS NULL
	`).Scan(&count); err != nil {
		t.Fatalf("count real users: %v", err)
	}

	id, ok := s.soleRealUserID()
	if count == 1 {
		if !ok {
			t.Error("soleRealUserID() = false, want true when exactly one real user exists")
		}
	} else {
		if ok {
			t.Errorf("soleRealUserID() = true (id %q), want false with %d real users", id, count)
		}
	}

	// A second real user must flip a currently-true answer to false: the one
	// transition the whole feature depends on noticing without anyone having
	// to turn a setting off by hand.
	if count == 1 && ok {
		seedUser(t, db, "agora361_second")
		if _, stillOK := s.soleRealUserID(); stillOK {
			t.Error("soleRealUserID() stayed true after a second real user was added")
		}
	}
}

// TestClaimInstanceDomainGating covers the refusals that don't depend on the
// instance actually being single-user, since the shared test database
// already has far more than one real user and there is no safe way to make
// it fewer for this test's duration.
func TestClaimInstanceDomainGating(t *testing.T) {
	db := testDB(t)
	s := testService(db)
	userID, _ := seedUser(t, db, "agora361_gating")

	authedRequest := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/custom-domain/use-instance-domain", nil)
		return req.WithContext(context.WithValue(req.Context(), ctxkeys.UserID, userID))
	}

	t.Run("switch off", func(t *testing.T) {
		setInstanceSetting(t, db, "single_user_domain_handle", "false")
		w := httptest.NewRecorder()
		s.ClaimInstanceDomain(w, authedRequest())
		if w.Code != 403 {
			t.Errorf("status = %d, want 403 with the switch off", w.Code)
		}
	})

	t.Run("more than one real user", func(t *testing.T) {
		var count int
		db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_remote = false AND deletion_scheduled_at IS NULL`).Scan(&count)
		if count == 1 {
			t.Skip("this test database happens to have exactly one real user right now")
		}
		setInstanceSetting(t, db, "single_user_domain_handle", "true")
		w := httptest.NewRecorder()
		s.ClaimInstanceDomain(w, authedRequest())
		if w.Code != 409 {
			t.Errorf("status = %d, want 409 with more than one real user", w.Code)
		}
	})
}

// TestAssertClaimableBlocksOtherAccounts is AGORA-290's core rule: while one
// account holds a domain, another can't. A verified claim is held
// indefinitely; an unverified one — which took no proof to make, and so would
// otherwise be a free permanent block on the domain's real owner — is only
// held until it goes stale.
func TestAssertClaimableBlocksOtherAccounts(t *testing.T) {
	db := testDB(t)
	s := testService(db)

	holderID, _ := seedUser(t, db, "agora290_holder")
	otherID, _ := seedUser(t, db, "agora290_other")
	domain := fmt.Sprintf("claim-%d.example", time.Now().UnixNano())

	if _, err := db.Exec(`
		INSERT INTO custom_domains (user_id, domain, protocol, verification_status)
		VALUES ($1, $2, 'atproto', 'verified')
	`, holderID, domain); err != nil {
		t.Fatalf("seed claim: %v", err)
	}

	if err := s.assertClaimable(domain, otherID); err == nil {
		t.Error("another account was allowed to claim a verified domain")
	}
	if err := s.assertClaimable(domain, holderID); err != nil {
		t.Errorf("the holder could not re-claim their own domain: %v", err)
	}

	// A fresh but unverified claim still blocks others...
	db.Exec(`UPDATE custom_domains SET verification_status = 'pending', updated_at = NOW() WHERE domain = $1`, domain)
	if err := s.assertClaimable(domain, otherID); err == nil {
		t.Error("another account was allowed to claim a freshly-claimed domain")
	}

	// ...until it goes stale, at which point it's taken over rather than
	// leaving the domain locked up by someone who never proved anything.
	db.Exec(`UPDATE custom_domains SET updated_at = NOW() - INTERVAL '30 days' WHERE domain = $1`, domain)
	if err := s.assertClaimable(domain, otherID); err != nil {
		t.Errorf("a stale unverified claim was not reclaimable: %v", err)
	}
	var remaining int
	db.QueryRow(`SELECT COUNT(*) FROM custom_domains WHERE domain = $1`, domain).Scan(&remaining)
	if remaining != 0 {
		t.Errorf("stale claim was not cleared, %d row(s) remain", remaining)
	}
}

// TestVerifyWellKnownRejectsMismatches exercises the HTTPS half of the
// challenge (AGORA-280) against a stub origin. verifyHTTPClient is swapped for
// a plain one because the real client deliberately refuses to dial loopback —
// the SSRF guard that TestWellKnownRefusesPrivateAddresses covers separately.
func TestVerifyWellKnownRejectsMismatches(t *testing.T) {
	const did = "did:web:someone.agora.example"

	cases := []struct {
		name    string
		handler http.HandlerFunc
		wantOK  bool
	}{
		{"exact match", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, did)
		}, true},
		{"surrounding whitespace is tolerated", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "\n  "+did+"  \n")
		}, true},
		{"a different DID", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "did:web:someone-else.agora.example")
		}, false},
		{"empty file", func(w http.ResponseWriter, r *http.Request) {}, false},
		{"not found", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(404)
		}, false},
		{"a redirect elsewhere", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://elsewhere.example/.well-known/atproto-did", 302)
		}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			// The stub is plain HTTP on loopback, so point the client at it
			// directly rather than at the https://<domain> URL under test.
			orig := verifyHTTPClient
			verifyHTTPClient = &http.Client{
				Transport: rewriteTransport{target: srv.Listener.Addr().String()},
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}
			defer func() { verifyHTTPClient = orig }()

			err := verifyWellKnown(context.Background(), "example.com", did)
			if tc.wantOK && err != nil {
				t.Errorf("expected verification to succeed, got: %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Error("expected verification to fail, but it succeeded")
			}
		})
	}
}

// rewriteTransport sends every request to the stub origin instead of resolving
// the domain under test, keeping the URL the code builds intact.
type rewriteTransport struct{ target string }

func (rt rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme = "http"
	r.URL.Host = rt.target
	return http.DefaultTransport.RoundTrip(r)
}

// TestWellKnownRefusesPrivateAddresses guards the SSRF protection directly:
// the well-known fetch targets a hostname the user typed, so a claim on a name
// resolving to loopback or a private range must not become a way to make the
// instance issue requests inside its own network.
func TestWellKnownRefusesPrivateAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "did:web:attacker.example")
	}))
	defer srv.Close()

	if err := verifyWellKnown(context.Background(), "localhost", "did:web:attacker.example"); err == nil {
		t.Error("verification against a loopback host succeeded, expected the SSRF guard to refuse it")
	}

	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "::1"} {
		if isPublicIP(parseIP(t, ip)) {
			t.Errorf("isPublicIP(%s) = true, want false", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !isPublicIP(parseIP(t, ip)) {
			t.Errorf("isPublicIP(%s) = false, want true", ip)
		}
	}
}

func parseIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("could not parse test IP %q", s)
	}
	return ip
}
