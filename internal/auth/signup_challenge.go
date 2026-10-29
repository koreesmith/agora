package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/httprate"
)

// AGORA-407: a self-hosted proof-of-work check on signup, in the ALTCHA
// shape. The server picks a random salt and a secret number, and hands out
// sha256(salt+number) plus an HMAC signature of it; the client finds the
// number by brute force, which costs a browser a second or two in the
// background and a bot farm real CPU per account. Nothing leaves the
// instance and there are no keys to configure.
//
// A signup without a valid solution is not rejected. It lands on the
// waitlist for an admin to approve, because old mobile builds can't solve
// the challenge and there is no forced-update gate to make them.

const (
	// challengeMaxNumber bounds the secret number, so a solve averages
	// half this many SHA-256 hashes.
	challengeMaxNumber = 100000
	// challengeLifetime is how long a challenge stays solvable, which is
	// also how long a person can take over the signup form.
	challengeLifetime = time.Hour
	// challengeMinAge is the minimum time between a challenge being issued
	// and the signup using it. People take longer than this to fill in the
	// form; scripts posting straight away don't.
	challengeMinAge = 3 * time.Second
)

var signupChallengeRateLimit = httprate.LimitByIP(60, time.Hour)

// challengeNow is swapped out by tests.
var challengeNow = time.Now

type signupChallenge struct {
	Algorithm string `json:"algorithm"`
	Challenge string `json:"challenge"`
	MaxNumber int    `json:"maxnumber"`
	Salt      string `json:"salt"`
	Signature string `json:"signature"`
}

type signupSolution struct {
	Algorithm string `json:"algorithm"`
	Challenge string `json:"challenge"`
	Number    int    `json:"number"`
	Salt      string `json:"salt"`
	Signature string `json:"signature"`
}

func (s *Service) signupChallengeEnabled() bool {
	return s.getSetting("signup_challenge") != "off"
}

// challengeKey derives the HMAC key from the instance's JWT secret, so
// there's no extra secret to manage, while keeping the two uses separate.
func (s *Service) challengeKey() []byte {
	k := sha256.Sum256([]byte("agora-signup-challenge:" + s.cfg.JWTSecret))
	return k[:]
}

func (s *Service) signChallenge(challenge string) string {
	m := hmac.New(sha256.New, s.challengeKey())
	m.Write([]byte(challenge))
	return hex.EncodeToString(m.Sum(nil))
}

func sha256Hex(v string) string {
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:])
}

func (s *Service) newSignupChallenge() (signupChallenge, error) {
	random, err := randomHex(12)
	if err != nil {
		return signupChallenge{}, err
	}
	n, err := randomHex(4)
	if err != nil {
		return signupChallenge{}, err
	}
	secret, _ := strconv.ParseUint(n, 16, 32)
	number := int(secret % challengeMaxNumber)

	// The issue and expiry times ride in the salt, which the challenge hash
	// (and so the signature) covers, so a client can't stretch either.
	now := challengeNow()
	salt := random + "?" + url.Values{
		"issued":  {strconv.FormatInt(now.Unix(), 10)},
		"expires": {strconv.FormatInt(now.Add(challengeLifetime).Unix(), 10)},
	}.Encode()
	challenge := sha256Hex(salt + strconv.Itoa(number))
	return signupChallenge{
		Algorithm: "SHA-256",
		Challenge: challenge,
		MaxNumber: challengeMaxNumber,
		Salt:      salt,
		Signature: s.signChallenge(challenge),
	}, nil
}

// SignupChallenge hands the signup form a puzzle to solve, or says the
// check is switched off so the client can skip the work.
func (s *Service) SignupChallenge(w http.ResponseWriter, r *http.Request) {
	if !s.signupChallengeEnabled() {
		writeJSON(w, 200, map[string]any{"enabled": false})
		return
	}
	c, err := s.newSignupChallenge()
	if err != nil {
		writeError(w, 500, "server error"); return
	}
	writeJSON(w, 200, map[string]any{"enabled": true, "challenge": c})
}

// verifySignupSolution checks a solution's maths, signature and timing.
// It doesn't consume it; consumeSignupSolution does that once the signup
// is otherwise good to go.
func (s *Service) verifySignupSolution(sol *signupSolution) bool {
	if sol == nil || sol.Algorithm != "SHA-256" || sol.Number < 0 || sol.Number > challengeMaxNumber {
		return false
	}
	if !hmac.Equal([]byte(s.signChallenge(sol.Challenge)), []byte(sol.Signature)) {
		return false
	}
	if sha256Hex(sol.Salt+strconv.Itoa(sol.Number)) != sol.Challenge {
		return false
	}
	_, query, ok := strings.Cut(sol.Salt, "?")
	if !ok {
		return false
	}
	params, err := url.ParseQuery(query)
	if err != nil {
		return false
	}
	issued, err1 := strconv.ParseInt(params.Get("issued"), 10, 64)
	expires, err2 := strconv.ParseInt(params.Get("expires"), 10, 64)
	if err1 != nil || err2 != nil {
		return false
	}
	now := challengeNow()
	if now.After(time.Unix(expires, 0)) || now.Before(time.Unix(issued, 0).Add(challengeMinAge)) {
		return false
	}
	return true
}

// consumeSignupSolution marks a verified solution used, and reports false
// if it already was, so one solve can't create a batch of accounts.
func (s *Service) consumeSignupSolution(sol *signupSolution) bool {
	_, query, _ := strings.Cut(sol.Salt, "?")
	params, _ := url.ParseQuery(query)
	expires, _ := strconv.ParseInt(params.Get("expires"), 10, 64)
	s.db.Exec(`DELETE FROM used_signup_challenges WHERE expires_at < NOW()`)
	res, err := s.db.Exec(`
		INSERT INTO used_signup_challenges (signature, expires_at)
		VALUES ($1, to_timestamp($2))
		ON CONFLICT (signature) DO NOTHING
	`, sol.Signature, expires)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}
