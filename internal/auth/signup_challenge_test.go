package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/internal/store"
)

// solve brute-forces a challenge the way the web client does.
func solve(t *testing.T, c signupChallenge) *signupSolution {
	t.Helper()
	for n := 0; n <= c.MaxNumber; n++ {
		if sha256Hex(c.Salt+strconv.Itoa(n)) == c.Challenge {
			return &signupSolution{Algorithm: c.Algorithm, Challenge: c.Challenge, Number: n, Salt: c.Salt, Signature: c.Signature}
		}
	}
	t.Fatal("challenge has no solution within maxnumber")
	return nil
}

// withClock pins challengeNow for one test.
func withClock(t *testing.T, at time.Time) *time.Time {
	t.Helper()
	now := at
	challengeNow = func() time.Time { return now }
	t.Cleanup(func() { challengeNow = time.Now })
	return &now
}

// AGORA-407: the maths, signature and timing checks need no database.
func TestVerifySignupSolution(t *testing.T) {
	s := &Service{cfg: &config.Config{JWTSecret: "agora407-test-secret"}}
	now := withClock(t, time.Unix(1_800_000_000, 0))
	c, err := s.newSignupChallenge()
	if err != nil {
		t.Fatal(err)
	}
	good := solve(t, c)

	at := func(d time.Duration) { *now = time.Unix(1_800_000_000, 0).Add(d) }

	at(10 * time.Second)
	if !s.verifySignupSolution(good) {
		t.Fatal("a correct solution 10s after issue was refused")
	}

	at(time.Second)
	if s.verifySignupSolution(good) {
		t.Error("a solution 1s after issue passed; it should be too fast for a person")
	}

	at(challengeLifetime + time.Second)
	if s.verifySignupSolution(good) {
		t.Error("an expired challenge passed")
	}

	at(10 * time.Second)
	wrongNumber := *good
	wrongNumber.Number = (good.Number + 1) % challengeMaxNumber
	stretchedSalt := *good
	stretchedSalt.Salt = strings.Replace(good.Salt, "expires=", "expires=9", 1)
	forged := *good
	forged.Signature = strings.Repeat("0", 64)
	otherInstance := &Service{cfg: &config.Config{JWTSecret: "some-other-secret"}}
	for name, sol := range map[string]*signupSolution{
		"nil":            nil,
		"wrong number":   &wrongNumber,
		"stretched salt": &stretchedSalt,
		"forged sig":     &forged,
	} {
		if s.verifySignupSolution(sol) {
			t.Errorf("%s: passed verification", name)
		}
	}
	if otherInstance.verifySignupSolution(good) {
		t.Error("a challenge signed by one instance verified on another")
	}
}

// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestRegisterSignupChallenge(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	// Registered first so it runs last, after the row cleanups below.
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if mode := (&Service{db: db}).getSetting("registration_mode"); mode != "open" {
		t.Skipf("skipping: test instance registration_mode is %q, not open", mode)
	}
	if (&Service{db: db}).getSetting("signup_challenge") != "on" {
		t.Fatal("signup_challenge was not seeded 'on' by Migrate")
	}
	cfg := &config.Config{JWTSecret: "agora407-test-secret"}
	s := &Service{db: db, cfg: cfg, notifSvc: notifications.NewService(db, notifications.NewEmailService(db, cfg))}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	challenge := func() signupChallenge {
		w := httptest.NewRecorder()
		s.SignupChallenge(w, httptest.NewRequest(http.MethodGet, "/auth/signup-challenge", nil))
		var res struct {
			Enabled   bool            `json:"enabled"`
			Challenge signupChallenge `json:"challenge"`
		}
		json.Unmarshal(w.Body.Bytes(), &res)
		if !res.Enabled {
			t.Fatalf("challenge endpoint says disabled: %s", w.Body)
		}
		return res.Challenge
	}
	register := func(tag string, sol *signupSolution) (string, string) {
		name := "agora407" + tag + suffix
		t.Cleanup(func() {
			db.Exec(`DELETE FROM notifications WHERE actor_id = (SELECT id FROM users WHERE username = $1)`, name)
			db.Exec(`DELETE FROM users WHERE username = $1`, name)
		})
		body, _ := json.Marshal(map[string]any{
			"username": name, "email": name + "@gmail.com", "password": "password123", "signup_challenge": sol,
		})
		w := httptest.NewRecorder()
		s.Register(w, httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(string(body))))
		if w.Code != 201 {
			t.Fatalf("%s: status %d body %s", tag, w.Code, w.Body)
		}
		var status string
		db.QueryRow(`SELECT waitlist_status FROM users WHERE username = $1`, name).Scan(&status)
		var res map[string]string
		json.Unmarshal(w.Body.Bytes(), &res)
		return status, res["message"]
	}

	now := withClock(t, time.Now())
	sol := solve(t, challenge())
	*now = now.Add(5 * time.Second)

	if status, msg := register("ok", sol); status != "" || msg == "waitlist" {
		t.Errorf("solved signup: waitlist_status %q, message %q; want a normal account", status, msg)
	}
	if status, msg := register("replay", sol); status != "pending" || msg != "waitlist" {
		t.Errorf("replayed solution: waitlist_status %q, message %q; want the waitlist", status, msg)
	}
	if status, msg := register("none", nil); status != "pending" || msg != "waitlist" {
		t.Errorf("no solution (old mobile build): waitlist_status %q, message %q; want the waitlist", status, msg)
	}
	// Let the background admin notifications land before cleanup removes them.
	time.Sleep(200 * time.Millisecond)
}
