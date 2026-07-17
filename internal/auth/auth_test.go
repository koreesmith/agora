package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// AGORA-142: isValidEmail replaced a bare strings.Contains(email, "@") check
// that let CR/LF (and other header-injection-capable) addresses through
// registration/invite/email-change, which notifications.go's SendHTML later
// spliced raw into SMTP headers.
func TestIsValidEmail(t *testing.T) {
	cases := []struct {
		name  string
		email string
		want  bool
	}{
		{"ordinary address", "user@example.com", true},
		{"subdomain and plus tag", "user+tag@mail.example.co.uk", true},
		{"missing @", "userexample.com", false},
		{"empty", "", false},
		{"CRLF header injection attempt", "victim@x.com\r\nBcc: mass@list.com", false},
		{"bare LF", "victim@x.com\nBcc: mass@list.com", false},
		{"trailing CRLF", "victim@x.com\r\n", false},
		{"display-name-wrapped address rejected (not a bare address)", "Attacker <victim@x.com>", false},
		{"whitespace-only", "   ", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isValidEmail(tc.email); got != tc.want {
				t.Errorf("isValidEmail(%q) = %v, want %v", tc.email, got, tc.want)
			}
		})
	}
}

// AGORA-141: /auth/login had no throttling at all, making it brute-forceable.
// Malformed JSON makes Login return 400 before it ever touches the (here
// nil) db, so this exercises only the rate-limit middleware wired up in
// RegisterPublicRoutes, not the handler's own logic.
func TestLoginIsRateLimited(t *testing.T) {
	s := &Service{}
	r := chi.NewRouter()
	RegisterPublicRoutes(r, s)

	newReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader("not json"))
		req.RemoteAddr = "203.0.113.7:12345"
		return req
	}

	var last *httptest.ResponseRecorder
	for i := 0; i < 11; i++ {
		last = httptest.NewRecorder()
		r.ServeHTTP(last, newReq())
	}
	if last.Code != http.StatusTooManyRequests {
		t.Errorf("11th /auth/login request from the same IP within a minute: got status %d, want %d", last.Code, http.StatusTooManyRequests)
	}
}
