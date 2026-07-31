package domains

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/httprate"

	"github.com/agora-social/agora/internal/auth"
)

// limitByUser throttles per account rather than per IP. Both endpoints it
// guards make the instance perform outbound lookups against a name the caller
// supplied, so the account is the meaningful unit — an IP limit alone leaves
// one account free to churn through a botnet, and punishes everyone behind a
// shared NAT for one user's retries (AGORA-290).
func limitByUser(requests int, window time.Duration) func(http.Handler) http.Handler {
	return httprate.Limit(requests, window, httprate.WithKeyFuncs(func(r *http.Request) (string, error) {
		return auth.UserIDFromCtx(r.Context()), nil
	}))
}

// normalizeDomain turns whatever the user typed into the bare hostname the
// challenge will actually be run against, or explains why it can't be one.
// Being liberal about the input (a pasted URL, a stray @, mixed case, a
// trailing dot) and strict about the stored value keeps the domain in the
// generated DNS instructions identical to the domain verifyDomain looks up.
func (s *Service) normalizeDomain(raw string) (string, error) {
	d := strings.ToLower(strings.TrimSpace(raw))
	d = strings.TrimPrefix(d, "@")
	d = strings.TrimPrefix(d, "https://")
	d = strings.TrimPrefix(d, "http://")
	d = strings.Split(d, "/")[0] // drop any path
	d = strings.Split(d, "?")[0]
	if i := strings.IndexByte(d, ':'); i != -1 {
		d = d[:i] // drop a port
	}
	d = strings.TrimSuffix(d, ".") // a fully-qualified trailing dot is valid DNS, but not a handle

	if d == "" {
		return "", fmt.Errorf("enter a domain")
	}
	if len(d) > 253 {
		return "", fmt.Errorf("that domain is too long")
	}
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return "", fmt.Errorf("enter a full domain, like example.com")
	}
	for _, label := range labels {
		if label == "" {
			return "", fmt.Errorf("that doesn't look like a valid domain")
		}
		if len(label) > 63 {
			return "", fmt.Errorf("that doesn't look like a valid domain")
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", fmt.Errorf("that doesn't look like a valid domain")
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '-' {
				return "", fmt.Errorf("that doesn't look like a valid domain")
			}
		}
	}
	// An all-numeric last label means an IP address, not a domain. AT Proto
	// handles are explicitly hostnames, and an IP has no owner to verify.
	if tld := labels[len(labels)-1]; strings.IndexFunc(tld, func(c rune) bool { return c < '0' || c > '9' }) == -1 {
		return "", fmt.Errorf("enter a domain name, not an IP address")
	}

	// The instance's own domain and everything under it is where the
	// auto-generated per-user handles live (username.instance-domain). Letting
	// a user claim inside it would let them mint a handle that collides with
	// another account's, or with the instance itself — the one impersonation
	// risk DNS verification genuinely cannot rule out, since the instance's
	// operator controls those records, not the claimant.
	instance := domainFromURL(s.cfg.InstanceDomain)
	if d == instance || strings.HasSuffix(d, "."+instance) {
		return "", fmt.Errorf("that domain belongs to this instance — you already have a handle there")
	}

	return d, nil
}

// reverifyInterval is how stale a verified claim may get before the sweep
// re-checks it, and recheckTick how often the sweep looks for stale ones.
// Twice a day is a deliberate floor rather than a ceiling: a lapsed domain
// costs the user their handle until it's noticed, but re-resolving every
// claim aggressively is the same outbound-lookup load AGORA-290 rate-limits
// users for, just self-inflicted.
const (
	reverifyInterval = 12 * time.Hour
	recheckTick      = time.Hour
	recheckBatch     = 100
)

// StartReverification is AGORA-280's "re-run periodically" half and the
// mechanism behind AGORA-289: a domain whose TXT record is deleted, whose
// registration lapses, or whose web server stops serving the well-known file
// stops being live here, without anyone having to notice and act.
//
// Only claims that have already been verified once (or verified and since
// failed, which may self-heal when the owner puts the record back) are
// re-checked. A claim that has never verified is left alone — the user hasn't
// created the record yet, and repeatedly resolving names on their behalf is
// exactly the load the claim rate limit exists to prevent.
func (s *Service) StartReverification(ctx context.Context) {
	ticker := time.NewTicker(recheckTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.recheckStale(ctx)
		}
	}
}

func (s *Service) recheckStale(ctx context.Context) {
	rows, err := s.db.Query(`
		SELECT `+claimCols+`, user_id
		FROM custom_domains
		WHERE protocol = $1
		  AND verification_status IN ('verified', 'failed')
		  AND (last_checked_at IS NULL OR last_checked_at < NOW() - $2::interval)
		ORDER BY last_checked_at ASC NULLS FIRST
		LIMIT $3
	`, ProtocolATProto, fmt.Sprintf("%d seconds", int(reverifyInterval.Seconds())), recheckBatch)
	if err != nil {
		log.Printf("domains: reverification query failed: %v", err)
		return
	}

	type pending struct {
		claim  *Claim
		userID string
	}
	var due []pending
	for rows.Next() {
		var c Claim
		var userID string
		if err := rows.Scan(&c.ID, &c.Domain, &c.VerificationMethod, &c.VerificationStatus,
			&c.ApprovalStatus, &c.LastError, &c.RejectionReason, &c.VerifiedAt,
			&c.LastCheckedAt, &c.CreatedAt, &userID); err != nil {
			continue
		}
		c.Live = isLive(c.VerificationStatus, c.ApprovalStatus)
		due = append(due, pending{claim: &c, userID: userID})
	}
	rows.Close()

	// The rows are collected before any checking starts: each check makes
	// network calls that take seconds, and holding a result set open across
	// all of them would pin a connection from the pool for the whole sweep.
	for _, p := range due {
		select {
		case <-ctx.Done():
			return
		default:
		}
		var did string
		if err := s.db.QueryRow(`SELECT COALESCE(atproto_did, '') FROM users WHERE id = $1`, p.userID).Scan(&did); err != nil || did == "" {
			continue
		}
		if _, err := s.runCheck(ctx, p.userID, p.claim, did, true); err != nil {
			log.Printf("domains: reverification of %s failed to record: %v", p.claim.Domain, err)
		}
	}
}
