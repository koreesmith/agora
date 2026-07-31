// Package domains implements "bring your own domain" — a user proving they
// control a domain so it can stand in for their instance-issued handle
// (AGORA-278).
//
// It is deliberately protocol-agnostic. The verification challenge it runs is
// AT Proto's (a DNS TXT record or a well-known file naming the user's DID),
// and today AT Proto is the only consumer, but nothing here knows what a
// verified domain is *for*: internal/atproto reads the table to decide what
// its DID documents claim, and the future ActivityPub custom-domain epic
// (AGORA-279) is expected to read the same rows for its own purposes rather
// than grow a parallel copy of the claim/verify/approve workflow.
package domains

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// ProtocolATProto is the only protocol AGORA-278 writes. The column exists so
// AGORA-279 can add its own value without a second table; every query in this
// package is scoped by it so the two can't collide.
const ProtocolATProto = "atproto"

// Approval modes for the instance-wide custom_domain_approval setting
// (AGORA-285).
const (
	ApprovalAuto   = "auto"
	ApprovalManual = "manual"
)

type Service struct {
	db    *store.DB
	cfg   *config.Config
	notif notifier
	atp   atprotoIdentity
}

type notifier interface {
	Create(userID, actorID, notifType, postID, data string)
}

// atprotoIdentity is the narrow slice of internal/atproto this package needs.
// It's an interface (wired by main, like users.SetAtproto) rather than a
// direct import because internal/atproto reads custom_domains itself —
// importing it here would be a cycle.
type atprotoIdentity interface {
	// EnsureUserDID returns the user's canonical did:web identifier, minting
	// and persisting their AT Proto identity if nothing has needed it yet.
	// Errors when AT Proto is off instance-wide or for that account, which is
	// what makes an AT Proto custom handle meaningless.
	EnsureUserDID(userID string) (string, error)
	// AnnounceHandleChange emits a firehose #identity event so relays and
	// AppViews re-resolve the account instead of serving the old handle until
	// their cache happens to expire. Called whenever a custom handle goes live
	// or stops being live.
	AnnounceHandleChange(userID string)
}

func NewService(db *store.DB, cfg *config.Config, notif notifier) *Service {
	return &Service{db: db, cfg: cfg, notif: notif}
}

func (s *Service) SetAtproto(a atprotoIdentity) { s.atp = a }

// RegisterRoutes wires the authenticated, user-facing surface (AGORA-284).
// The rate limits are AGORA-290's: both endpoints make the instance perform
// outbound DNS and HTTPS lookups against a name the caller chose, so they're
// throttled per account rather than only per IP.
func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/custom-domain", s.GetMine)
	r.With(limitByUser(10, time.Hour)).Post("/custom-domain", s.Claim)
	r.With(limitByUser(20, time.Hour)).Post("/custom-domain/verify", s.Verify)
	r.Delete("/custom-domain", s.Release)
}

// RegisterAdminRoutes wires the approval queue (AGORA-286). Mounted by main
// under the admin-only group, so these never re-check the caller's role.
func RegisterAdminRoutes(r chi.Router, s *Service) {
	r.Get("/admin/custom-domains", s.AdminList)
	r.Post("/admin/custom-domains/{id}/approve", s.AdminApprove)
	r.Post("/admin/custom-domains/{id}/reject", s.AdminReject)
}

// ── Model ─────────────────────────────────────────────────────────────────────

type Claim struct {
	ID                 string  `json:"id"`
	Domain             string  `json:"domain"`
	VerificationMethod string  `json:"verification_method"`
	VerificationStatus string  `json:"verification_status"`
	ApprovalStatus     string  `json:"approval_status"`
	// Live is the one field the UI should branch on for "is this actually my
	// handle right now" — it's the conjunction of both status axes, computed
	// in one place so no caller has to remember that verified-but-unapproved
	// and approved-but-no-longer-verified both mean "not live."
	Live               bool    `json:"live"`
	LastError          string  `json:"last_error"`
	RejectionReason    string  `json:"rejection_reason"`
	VerifiedAt         *string `json:"verified_at"`
	LastCheckedAt      *string `json:"last_checked_at"`
	CreatedAt          string  `json:"created_at"`
}

func isLive(verification, approval string) bool {
	return verification == "verified" && approval == "approved"
}

const claimCols = `id, domain, verification_method, verification_status, approval_status,
	last_error, rejection_reason, verified_at, last_checked_at, created_at`

func scanClaim(row interface{ Scan(...any) error }) (*Claim, error) {
	var c Claim
	if err := row.Scan(&c.ID, &c.Domain, &c.VerificationMethod, &c.VerificationStatus,
		&c.ApprovalStatus, &c.LastError, &c.RejectionReason, &c.VerifiedAt,
		&c.LastCheckedAt, &c.CreatedAt); err != nil {
		return nil, err
	}
	c.Live = isLive(c.VerificationStatus, c.ApprovalStatus)
	return &c, nil
}

func (s *Service) claimForUser(userID string) (*Claim, error) {
	return scanClaim(s.db.QueryRow(`
		SELECT `+claimCols+` FROM custom_domains WHERE user_id = $1 AND protocol = $2
	`, userID, ProtocolATProto))
}

// approvalMode reads the instance-wide setting, treating anything other than
// an explicit "auto" as manual — the same absent-key-means-the-safe-option
// convention atprotoEnabled uses for its own kill switch.
func (s *Service) approvalMode() string {
	var val string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'custom_domain_approval'`).Scan(&val)
	if val == ApprovalAuto {
		return ApprovalAuto
	}
	return ApprovalManual
}

// fallbackHandle is the instance-issued handle a user has whether or not they
// ever claim a domain, and the one they fall back to if a claim lapses
// (AGORA-289). Mirrors internal/atproto's own derivation.
func (s *Service) fallbackHandle(username string) string {
	return username + "." + domainFromURL(s.cfg.InstanceDomain)
}

func domainFromURL(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	return strings.Split(u, "/")[0]
}

// ── User-facing handlers ──────────────────────────────────────────────────────

// GetMine returns the caller's claim (if any) alongside everything the
// settings panel needs to render setup instructions: their DID, the handle
// they have today, and whether a verified claim still needs admin review.
func (s *Service) GetMine(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	var username string
	if err := s.db.QueryRow(`SELECT username FROM users WHERE id = $1`, userID).Scan(&username); err != nil {
		writeError(w, 404, "user not found")
		return
	}

	resp := map[string]any{
		"approval_mode":   s.approvalMode(),
		"fallback_handle": s.fallbackHandle(username),
		"current_handle":  s.fallbackHandle(username),
		"claim":           nil,
		"available":       true,
	}

	// A DID the user doesn't have yet is not an error here — it just means
	// nothing has needed their AT Proto identity so far. Minting it on this
	// read is what lets the panel show a copy-pasteable DNS record before the
	// user has done anything at all.
	did, err := s.atp.EnsureUserDID(userID)
	if err != nil {
		resp["available"] = false
		resp["unavailable_reason"] = err.Error()
		writeJSON(w, 200, resp)
		return
	}
	resp["did"] = did

	claim, err := s.claimForUser(userID)
	if err == sql.ErrNoRows {
		writeJSON(w, 200, resp)
		return
	}
	if err != nil {
		writeError(w, 500, "db error")
		return
	}

	resp["claim"] = claim
	resp["instructions"] = instructionsFor(claim.Domain, did)
	if claim.Live {
		resp["current_handle"] = claim.Domain
	}
	writeJSON(w, 200, resp)
}

// instructionsFor renders the exact record the user has to create. Built
// server-side rather than assembled in the frontend so there is one authority
// on what verifyDomain will actually look for — a mismatch between the two
// would present as "I did exactly what you told me and it says it failed."
func instructionsFor(domain, did string) map[string]string {
	return map[string]string{
		"dns_record_type":    "TXT",
		"dns_record_name":    dnsRecordName + domain,
		"dns_record_value":   "did=" + did,
		"well_known_url":     "https://" + domain + wellKnownPath,
		"well_known_content": did,
	}
}

// Claim registers (or replaces) the caller's domain claim. It does not verify
// anything: the user hasn't created the DNS record yet at this point, so an
// immediate check would fail by construction and read as an error rather than
// the next step. Verify is a separate, explicit action.
func (s *Service) Claim(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	var req struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json")
		return
	}

	domain, err := s.normalizeDomain(req.Domain)
	if err != nil {
		writeError(w, 422, err.Error())
		return
	}

	did, err := s.atp.EnsureUserDID(userID)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}

	if err := s.assertClaimable(domain, userID); err != nil {
		writeError(w, 409, err.Error())
		return
	}

	// Replacing rather than accumulating: one row per user per protocol (see
	// idx_custom_domains_user). Re-claiming the same domain resets its
	// verification state, which is the right behavior for a user retrying
	// after fixing their DNS — the alternative, leaving a stale "failed"
	// verdict attached, just makes the panel lie until the next check.
	var claim *Claim
	err = s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM custom_domains WHERE user_id = $1 AND protocol = $2`,
			userID, ProtocolATProto); err != nil {
			return err
		}
		var err error
		claim, err = scanClaim(tx.QueryRow(`
			INSERT INTO custom_domains (user_id, domain, protocol)
			VALUES ($1, $2, $3)
			RETURNING `+claimCols, userID, domain, ProtocolATProto))
		return err
	})
	if err != nil {
		// The unique index is the backstop against two accounts racing for the
		// same domain between assertClaimable and this insert.
		if isUniqueViolation(err) {
			writeError(w, 409, "that domain is already claimed by another account")
			return
		}
		log.Printf("domains: claim failed for user %s: %v", userID, err)
		writeError(w, 500, "could not save claim")
		return
	}

	writeJSON(w, 201, map[string]any{
		"claim":        claim,
		"did":          did,
		"instructions": instructionsFor(domain, did),
	})
}

// assertClaimable enforces AGORA-290's "not two accounts at once" rule. A
// claim nobody has proven and nobody has touched in staleClaimWindow is
// treated as abandoned and taken over, because the alternative is that an
// unverifiable claim — which by definition requires no proof to make — blocks
// the domain's real owner permanently.
func (s *Service) assertClaimable(domain, userID string) error {
	var holderID, verification string
	var updatedAt time.Time
	err := s.db.QueryRow(`
		SELECT user_id, verification_status, updated_at FROM custom_domains
		WHERE domain = $1 AND protocol = $2 AND approval_status <> 'rejected'
	`, domain, ProtocolATProto).Scan(&holderID, &verification, &updatedAt)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("could not check that domain")
	}
	if holderID == userID {
		return nil // re-claiming your own domain is a retry, not a conflict
	}
	if verification == "verified" {
		return fmt.Errorf("that domain is already verified by another account")
	}
	if time.Since(updatedAt) < staleClaimWindow {
		return fmt.Errorf("another account has an open claim on that domain — if it's yours, try again in a few days")
	}
	if _, err := s.db.Exec(`
		DELETE FROM custom_domains
		WHERE domain = $1 AND protocol = $2 AND user_id = $3 AND verification_status <> 'verified'
	`, domain, ProtocolATProto, holderID); err != nil {
		return fmt.Errorf("could not check that domain")
	}
	log.Printf("domains: %s reclaimed from stale unverified claim by user %s", domain, holderID)
	return nil
}

// Verify runs the challenge against the caller's claim and records the
// verdict. This is the on-demand half of AGORA-280; recheckAll is the
// periodic half.
func (s *Service) Verify(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	claim, err := s.claimForUser(userID)
	if err == sql.ErrNoRows {
		writeError(w, 404, "no custom domain claimed")
		return
	}
	if err != nil {
		writeError(w, 500, "db error")
		return
	}

	did, err := s.atp.EnsureUserDID(userID)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}

	updated, err := s.runCheck(r.Context(), userID, claim, did, false)
	if err != nil {
		writeError(w, 500, "could not record verification result")
		return
	}
	writeJSON(w, 200, map[string]any{"claim": updated, "instructions": instructionsFor(claim.Domain, did)})
}

// runCheck performs one verification attempt and applies the result,
// including the side effects a status change implies: auto-approval, going
// live or ceasing to be live on the firehose, and notifying the user.
//
// background distinguishes the periodic sweep from a user-initiated check.
// Only the sweep notifies on failure — a user who just pressed "Check
// verification" is already looking at the answer, and a notification about it
// would be noise (AGORA-287).
func (s *Service) runCheck(ctx context.Context, userID string, claim *Claim, did string, background bool) (*Claim, error) {
	wasLive := claim.Live
	method, verifyErr := s.verifyDomain(ctx, claim.Domain, did)

	var updated *Claim
	err := s.withTx(func(tx *sql.Tx) error {
		var err error
		if verifyErr != nil {
			updated, err = scanClaim(tx.QueryRow(`
				UPDATE custom_domains
				SET verification_status = 'failed', last_error = $1,
				    last_checked_at = NOW(), updated_at = NOW()
				WHERE id = $2
				RETURNING `+claimCols, verifyErr.Error(), claim.ID))
			return err
		}
		// Auto-approve only moves a claim that is still awaiting review; a
		// rejected claim staying rejected through a re-verification is the
		// point of the admin's decision (AGORA-285).
		autoApprove := s.approvalMode() == ApprovalAuto && claim.ApprovalStatus == "pending"
		updated, err = scanClaim(tx.QueryRow(`
			UPDATE custom_domains
			SET verification_status = 'verified', verification_method = $1, last_error = '',
			    verified_at = COALESCE(verified_at, NOW()), last_checked_at = NOW(), updated_at = NOW(),
			    approval_status = CASE WHEN $2 THEN 'approved' ELSE approval_status END,
			    reviewed_at     = CASE WHEN $2 THEN NOW()       ELSE reviewed_at     END
			WHERE id = $3
			RETURNING `+claimCols, method, autoApprove, claim.ID))
		return err
	})
	if err != nil {
		log.Printf("domains: could not record verification for %s: %v", claim.Domain, err)
		return nil, err
	}

	if updated.Live != wasLive {
		s.atp.AnnounceHandleChange(userID)
	}

	switch {
	case verifyErr != nil && wasLive:
		// AGORA-289: a handle that was live has stopped resolving — the record
		// was removed, or the domain lapsed. The account is already back on
		// its instance handle by this point (Live is false), so this tells the
		// user what happened rather than leaving them to notice it themselves.
		s.notif.Create(userID, "", "custom_domain_lost", "", updated.Domain)
	case verifyErr != nil && background && claim.VerificationStatus != "failed":
		// Only the transition into failure is worth a notification. The sweep
		// keeps re-checking a failed claim indefinitely (it may self-heal when
		// the owner restores the record), so notifying on every repeat failure
		// would be a twice-daily reminder of something the user already knows.
		s.notif.Create(userID, "", "custom_domain_failed", "", updated.Domain)
	case verifyErr == nil && !wasLive && updated.Live:
		s.notif.Create(userID, "", "custom_domain_live", "", updated.Domain)
	case verifyErr == nil && claim.VerificationStatus != "verified":
		// Verified but still queued for review — worth saying so explicitly,
		// otherwise "verified" reads as "done" and the wait looks like a bug.
		s.notif.Create(userID, "", "custom_domain_verified", "", updated.Domain)
	}
	return updated, nil
}

// Release drops the caller's claim entirely, reverting them to their instance
// handle.
func (s *Service) Release(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	claim, err := s.claimForUser(userID)
	if err == sql.ErrNoRows {
		writeJSON(w, 200, map[string]string{"message": "no custom domain claimed"})
		return
	}
	if err != nil {
		writeError(w, 500, "db error")
		return
	}

	if _, err := s.db.Exec(`DELETE FROM custom_domains WHERE id = $1 AND user_id = $2`, claim.ID, userID); err != nil {
		writeError(w, 500, "could not release domain")
		return
	}
	if claim.Live {
		s.atp.AnnounceHandleChange(userID)
	}
	writeJSON(w, 200, map[string]string{"message": "custom domain released"})
}

// ── Admin handlers (AGORA-286) ────────────────────────────────────────────────

type adminClaim struct {
	Claim
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	DID         string `json:"did"`
}

// AdminList returns the review queue. It defaults to exactly what needs a
// decision — verified but unapproved — with ?status=all for the full picture,
// since a rejected or still-unverified claim is context an admin sometimes
// wants but never has to act on.
func (s *Service) AdminList(w http.ResponseWriter, r *http.Request) {
	where := `d.verification_status = 'verified' AND d.approval_status = 'pending'`
	if r.URL.Query().Get("status") == "all" {
		where = `TRUE`
	}

	rows, err := s.db.Query(`
		SELECT d.id, d.domain, d.verification_method, d.verification_status, d.approval_status,
		       d.last_error, d.rejection_reason, d.verified_at, d.last_checked_at, d.created_at,
		       u.id, u.username, u.display_name, COALESCE(u.atproto_did, '')
		FROM custom_domains d
		JOIN users u ON u.id = d.user_id
		WHERE d.protocol = $1 AND `+where+`
		ORDER BY d.verified_at ASC NULLS LAST, d.created_at ASC
		LIMIT 200
	`, ProtocolATProto)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	claims := []adminClaim{}
	for rows.Next() {
		var c adminClaim
		if err := rows.Scan(&c.ID, &c.Domain, &c.VerificationMethod, &c.VerificationStatus,
			&c.ApprovalStatus, &c.LastError, &c.RejectionReason, &c.VerifiedAt, &c.LastCheckedAt,
			&c.CreatedAt, &c.UserID, &c.Username, &c.DisplayName, &c.DID); err != nil {
			continue
		}
		c.Live = isLive(c.VerificationStatus, c.ApprovalStatus)
		claims = append(claims, c)
	}
	writeJSON(w, 200, map[string]any{"domains": claims, "approval_mode": s.approvalMode()})
}

// AdminApprove makes a verified claim live. It refuses to approve a claim
// that isn't currently verified: approving one would put a handle into DID
// documents that the domain's DNS doesn't back, which is precisely the
// mismatch verification exists to prevent.
func (s *Service) AdminApprove(w http.ResponseWriter, r *http.Request) {
	actorID := auth.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var userID, domain, verification string
	err := s.db.QueryRow(`
		SELECT user_id, domain, verification_status FROM custom_domains WHERE id = $1 AND protocol = $2
	`, id, ProtocolATProto).Scan(&userID, &domain, &verification)
	if err == sql.ErrNoRows {
		writeError(w, 404, "domain claim not found")
		return
	}
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	if verification != "verified" {
		writeError(w, 422, "that claim is not currently verified — it cannot be approved yet")
		return
	}

	if _, err := s.db.Exec(`
		UPDATE custom_domains
		SET approval_status = 'approved', rejection_reason = '', reviewed_by = $1,
		    reviewed_at = NOW(), updated_at = NOW()
		WHERE id = $2
	`, actorID, id); err != nil {
		writeError(w, 500, "could not approve")
		return
	}

	s.db.Exec(`INSERT INTO audit_log (actor_id, action, target_type, target_id, details)
		VALUES ($1, 'approve_custom_domain', 'custom_domain', $2, $3)`, actorID, id, domain)

	s.atp.AnnounceHandleChange(userID)
	// No actor on the notification: which admin reviewed a request isn't the
	// user's business, and naming one invites them to be argued with
	// personally. Same reasoning moderation emails use.
	s.notif.Create(userID, "", "custom_domain_live", "", domain)

	writeJSON(w, 200, map[string]string{"message": "domain approved"})
}

func (s *Service) AdminReject(w http.ResponseWriter, r *http.Request) {
	actorID := auth.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var req struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	reason := strings.TrimSpace(req.Reason)
	if len(reason) > 500 {
		reason = reason[:500]
	}

	var userID, domain string
	var wasApproved bool
	err := s.db.QueryRow(`
		SELECT user_id, domain, approval_status = 'approved' FROM custom_domains
		WHERE id = $1 AND protocol = $2
	`, id, ProtocolATProto).Scan(&userID, &domain, &wasApproved)
	if err == sql.ErrNoRows {
		writeError(w, 404, "domain claim not found")
		return
	}
	if err != nil {
		writeError(w, 500, "db error")
		return
	}

	if _, err := s.db.Exec(`
		UPDATE custom_domains
		SET approval_status = 'rejected', rejection_reason = $1, reviewed_by = $2,
		    reviewed_at = NOW(), updated_at = NOW()
		WHERE id = $3
	`, reason, actorID, id); err != nil {
		writeError(w, 500, "could not reject")
		return
	}

	s.db.Exec(`INSERT INTO audit_log (actor_id, action, target_type, target_id, details)
		VALUES ($1, 'reject_custom_domain', 'custom_domain', $2, $3)`, actorID, id, domain+" — "+reason)

	// Rejecting an already-live handle is allowed (an admin may need to undo
	// an approval), and revokes it — so the firehose has to hear about it.
	if wasApproved {
		s.atp.AnnounceHandleChange(userID)
	}
	s.notif.Create(userID, "", "custom_domain_rejected", "", domain)

	writeJSON(w, 200, map[string]string{"message": "domain rejected"})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (s *Service) withTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// isUniqueViolation matches lib/pq's 23505 without importing the driver's
// error type — the rest of this codebase treats database/sql as its only
// database surface, and one string check is cheaper than breaking that.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate key value")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
