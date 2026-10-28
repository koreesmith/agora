package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
	"github.com/go-chi/chi/v5"
)

// AGORA-406: the popular providers must never be caught by the vendored
// disposable list, even if a future refresh of that file adds one of them
// by mistake.
func TestNeverBlockedDomainsSurviveBlocklist(t *testing.T) {
	if len(disposableDomains) < 1000 {
		t.Fatalf("disposable list parsed to only %d domains; is the embedded file empty?", len(disposableDomains))
	}
	for d := range neverBlockedDomains {
		if emailDomainBlocked("someone@"+d, "") {
			t.Errorf("%s is blocked, but it's a mainstream provider", d)
		}
		if disposableDomains[d] {
			t.Logf("note: upstream list now contains %s; neverBlockedDomains is overriding it", d)
		}
	}
}

func TestEmailDomainBlocked(t *testing.T) {
	cases := []struct {
		name, email, admin string
		want               bool
	}{
		{"gmail", "user@gmail.com", "", false},
		{"yahoo", "user@yahoo.com", "", false},
		{"proton.me", "user@proton.me", "", false},
		{"protonmail.com", "user@protonmail.com", "", false},
		{"uppercase gmail", "User@GMAIL.COM", "", false},
		{"ordinary custom domain", "me@koree.net", "", false},
		{"disposable", "bot@mailinator.com", "", true},
		{"disposable, uppercase", "bot@MAILINATOR.com", "", true},
		{"subdomain of disposable", "bot@x.mailinator.com", "", true},
		{"subdomain of allowed provider", "user@mail.gmail.com", "", false},
		{"admin list, comma separated", "spam@spammy.example", "a.example, spammy.example", true},
		{"admin list, newline separated with @", "spam@spammy.example", "a.example\n@spammy.example", true},
		{"admin list covers subdomains", "spam@x.spammy.example", "spammy.example", true},
		{"admin list beats the allowlist", "user@gmail.com", "gmail.com", true},
		{"no @", "nope", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := emailDomainBlocked(tc.email, tc.admin); got != tc.want {
				t.Errorf("emailDomainBlocked(%q, %q) = %v, want %v", tc.email, tc.admin, got, tc.want)
			}
		})
	}
}

// Malformed JSON makes Register return 400 before it touches the (nil) db,
// so this exercises only the rate-limit middleware.
func TestRegisterIsRateLimited(t *testing.T) {
	s := &Service{}
	r := chi.NewRouter()
	RegisterPublicRoutes(r, s)

	newReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader("not json"))
		req.RemoteAddr = "203.0.113.9:12345"
		return req
	}

	var last *httptest.ResponseRecorder
	for i := 0; i < 6; i++ {
		last = httptest.NewRecorder()
		r.ServeHTTP(last, newReq())
	}
	if last.Code != http.StatusTooManyRequests {
		t.Errorf("6th /auth/register request from the same IP within an hour: got status %d, want %d", last.Code, http.StatusTooManyRequests)
	}
}

// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestRegisterGates(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	// Registered first so it runs last, after the row cleanups below.
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := &Service{db: db}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.Register(w, httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(body)))
		return w
	}
	exists := func(username string) bool {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = $1`, username).Scan(&n)
		return n > 0
	}

	t.Run("filled hidden field looks like success but creates nothing", func(t *testing.T) {
		name := "agora406hp" + suffix
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE username = $1`, name) })
		w := post(fmt.Sprintf(`{"username":%q,"email":%q,"password":"password123","website":"http://spam.example"}`, name, name+"@gmail.com"))
		if w.Code != 201 {
			t.Fatalf("status %d, want 201 (body %s)", w.Code, w.Body)
		}
		if exists(name) {
			t.Error("a user was created even though the hidden field was filled in")
		}
	})

	t.Run("disposable address is rejected", func(t *testing.T) {
		name := "agora406dd" + suffix
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE username = $1`, name) })
		w := post(fmt.Sprintf(`{"username":%q,"email":%q,"password":"password123"}`, name, name+"@mailinator.com"))
		if w.Code != 400 || !strings.Contains(w.Body.String(), "email provider") {
			t.Fatalf("status %d body %s, want 400 about the email provider", w.Code, w.Body)
		}
		if exists(name) {
			t.Error("a user was created with a disposable address")
		}
	})
}
