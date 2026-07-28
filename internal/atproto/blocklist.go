package atproto

import "strings"

// isInstanceBlocked mirrors federation.Service's own method of the same
// name (internal/federation/federation.go) — duplicated rather than
// imported, the same tradeoff already made for domainFromURL (atproto.go's
// own doc comment): a small pure-ish query isn't worth a cross-package
// dependency between the two federation layers. Reused as-is for the
// PDS-host block scope (AGORA-205) — a domain is still meaningful at the
// transport layer even though AT Proto identity is DID-first, not
// domain-first the way a fediverse actor's is.
func (s *Service) isInstanceBlocked(domain string) bool {
	domain = strings.ToLower(domain)
	var blocked bool
	s.db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM federated_instances WHERE domain = $1 AND status = 'blocked')
		    OR EXISTS(SELECT 1 FROM instance_bans WHERE LOWER(instance) = $1)
	`, domain).Scan(&blocked)
	return blocked
}

// isDIDBlocked checks the DID-scoped block list (AGORA-205) — AT Proto's
// natural blockable unit, since a DID identifies one specific account
// rather than an instance/domain the way ActivityPub's actor URL does.
func (s *Service) isDIDBlocked(did string) bool {
	if did == "" {
		return false
	}
	var blocked bool
	s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM blocked_dids WHERE did = $1)`, did).Scan(&blocked)
	return blocked
}

// isBlueskyActorBlocked is the single enforcement point every inbound AT
// Proto path in this epic (AGORA-197's ingestion, AGORA-199's inbound
// replies, AGORA-200's inbound likes/reposts) checks before acting on
// content from a given actor — checked from each path's first version,
// not bolted on after the fact the way AGORA-148 found ActivityPub's
// instance-blocking gap.
//
// A Bluesky handle (e.g. "user.bsky.social", or a custom domain like
// "alice.example.com") is itself a domain, reused here as the PDS-host
// block scope's comparison key: this instance has no direct PDS-host
// resolution machinery (every read goes through the shared AppView, never
// a specific PDS), so the handle's own domain is the closest available
// proxy for "who hosts this account" without adding DID-document
// resolution just for this.
func (s *Service) isBlueskyActorBlocked(did, handle string) bool {
	if s.isDIDBlocked(did) {
		return true
	}
	if handle != "" && s.isInstanceBlocked(handle) {
		return true
	}
	return false
}
