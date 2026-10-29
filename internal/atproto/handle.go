package atproto

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/events"
)

// This file is the AT Proto half of "bring your own domain" (AGORA-278): what
// a verified custom domain actually *does* once internal/domains has proven
// the user controls it. That package owns the claim/verify/approve workflow
// and never touches identity; this one owns identity and never touches the
// workflow. They meet at the custom_domains table and the small interface
// domains.Service holds (EnsureUserDID/AnnounceHandleChange, implemented
// below), rather than importing each other — internal/domains needs a DID
// from here, and here needs a verified domain from there, so a direct import
// either way would be a cycle.

// customHandleFor returns the live custom-domain handle for a user, or "" if
// they have none. "Live" means both axes agree — DNS still verifies AND an
// admin has approved (or auto-approval did). Anything less falls back to the
// instance handle silently, which is what makes AGORA-289's lapsed-domain
// case a non-event here: the row stops satisfying this query and every
// identity surface reverts on its own, with no separate teardown path to get
// wrong.
func (s *Service) customHandleFor(userID string) string {
	var domain string
	err := s.db.QueryRow(`
		SELECT domain FROM custom_domains
		WHERE user_id = $1 AND protocol = 'atproto'
		  AND verification_status = 'verified' AND approval_status = 'approved'
	`, userID).Scan(&domain)
	if err != nil {
		return ""
	}
	return domain
}

// handlesFor lists every handle a user's DID claims, most-preferred first.
// Order is load-bearing: AT Proto treats the first alsoKnownAs entry as the
// account's primary handle, so a live custom domain has to lead. The
// instance handle stays in the list rather than being replaced — it never
// stops working, it's what the DID itself is derived from, and keeping it
// published means anyone who knew the account by it can still resolve it.
func (s *Service) handlesFor(userID, username string) []string {
	instanceHandle := username + "." + domainFromURL(s.cfg.InstanceDomain)
	if custom := s.customHandleFor(userID); custom != "" {
		return []string{custom, instanceHandle}
	}
	return []string{instanceHandle}
}

// userByCustomDomain resolves a live custom-domain host to its owner, for
// AGORA-283. Only users who would pass eligibleUser's own gates are returned:
// a suspended, private, or AT-Proto-disabled account must not become
// resolvable just because it happens to hold a verified domain.
func (s *Service) userByCustomDomain(host string) (*eligibleUser, bool) {
	if !s.atprotoEnabled() || host == "" {
		return nil, false
	}
	var u eligibleUser
	err := s.db.QueryRow(`
		SELECT u.id, u.username, u.atproto_did, u.atproto_private_key
		FROM custom_domains d
		JOIN users u ON u.id = d.user_id
		WHERE d.domain = LOWER($1) AND d.protocol = 'atproto'
		  AND d.verification_status = 'verified' AND d.approval_status = 'approved'
		  AND u.is_remote = false AND u.profile_private = false
		  AND u.atproto_enabled = true AND u.deletion_scheduled_at IS NULL
	`, host).Scan(&u.ID, &u.Username, &u.AtprotoDID, &u.AtprotoPriv)
	if err != nil {
		return nil, false
	}
	return &u, true
}

// ── The internal/domains-facing interface ─────────────────────────────────────

// ErrAtprotoDisabled is returned to internal/domains when AT Proto is off,
// instance-wide or for the account. A custom AT Proto handle is meaningless
// without it — nothing would ever resolve the handle — so the settings panel
// says so plainly instead of accepting a claim that could never take effect.
var ErrAtprotoDisabled = fmt.Errorf("Bluesky (AT Protocol) is turned off for this account or instance — a custom handle needs it on")

// EnsureUserDID returns a user's canonical did:web identifier, minting and
// persisting their signing key on first use. Custom-domain setup is usually
// the earliest thing that needs a DID — the well-known endpoints mint one
// lazily when a relay first asks, and a user setting up a domain is asking
// before anyone else has — so this exists to make that first read explicit
// rather than a side effect of serving someone else's request.
func (s *Service) EnsureUserDID(userID string) (string, error) {
	if !s.atprotoEnabled() {
		return "", ErrAtprotoDisabled
	}
	var username, storedDID, storedPriv string
	var enabled bool
	err := s.db.QueryRow(`
		SELECT username, atproto_did, atproto_private_key, atproto_enabled
		FROM users WHERE id = $1 AND is_remote = false
	`, userID).Scan(&username, &storedDID, &storedPriv, &enabled)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("user not found")
	}
	if err != nil {
		return "", fmt.Errorf("could not load account")
	}
	if !enabled {
		return "", ErrAtprotoDisabled
	}

	did, _, err := s.ensureIdentity(userID, username, storedDID, storedPriv)
	if err != nil {
		return "", fmt.Errorf("could not prepare your AT Protocol identity")
	}
	return did, nil
}

// AnnounceHandleChange emits a #identity firehose event (AGORA-282) so relays
// and AppViews re-resolve the account's handle now rather than whenever their
// own cache happens to lapse. Without it a newly-approved custom handle is
// technically correct in the DID document and invisible everywhere that
// matters, which reads to the user as the feature not working.
//
// Best-effort by design: it's called from request paths whose actual job
// (approving a claim, releasing a domain) has already succeeded and must not
// be failed by a firehose hiccup. The DID document is the source of truth
// either way, so the worst case of a dropped event is slower propagation.
func (s *Service) AnnounceHandleChange(userID string) {
	if !s.atprotoEnabled() {
		return
	}
	var username, did string
	if err := s.db.QueryRow(`
		SELECT username, COALESCE(atproto_did, '') FROM users WHERE id = $1
	`, userID).Scan(&username, &did); err != nil || did == "" {
		return
	}

	handle := s.handlesFor(userID, username)[0]
	evt := &events.XRPCStreamEvent{
		RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{
			Did:    did,
			Handle: &handle,
			Time:   time.Now().UTC().Format(time.RFC3339),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.persister.Persist(ctx, evt); err != nil {
		log.Printf("atproto: could not emit #identity for %s: %v", did, err)
		return
	}
	log.Printf("atproto: announced handle %s for %s", handle, did)
}
