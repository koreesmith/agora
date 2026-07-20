// Package atproto is Agora's native AT Protocol (Bluesky) layer — the v3.0.0
// counterpart to internal/federation's ActivityPub layer (AGORA-184).
//
// This package is the only place in Agora that imports
// github.com/bluesky-social/indigo directly (per the spike's recommendation,
// AGORA-185): every other package deals in this package's own DID/key types,
// never indigo's, so a future engine swap — if the dependency footprint or
// upstream direction ever makes that necessary — is contained to this one
// package plus a data migration, not a rewrite scattered across the app.
package atproto

import (
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/events"
	"github.com/go-chi/chi/v5"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/internal/store"
)

type Service struct {
	db     *store.DB
	cfg    *config.Config
	events *events.EventManager
	notif  *notifications.Service
}

func NewService(db *store.DB, cfg *config.Config, notif *notifications.Service) *Service {
	return &Service{db: db, cfg: cfg, events: events.NewEventManager(newPgEventPersister(db)), notif: notif}
}

// RegisterRoutes wires the public, unauthenticated AT Proto identity and
// sync endpoints — dereferenced directly by AT Proto clients/relays/AppViews
// at well-known/XRPC paths, mirroring how federation.RegisterRoutes exposes
// WebFinger/actor docs outside the /api prefix.
func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/.well-known/did.json", s.DIDDocument)
	r.Get("/.well-known/atproto-did", s.AtprotoDIDText)
	r.Get("/xrpc/com.atproto.sync.subscribeRepos", s.SubscribeRepos)
	// AGORA-230: a relay probes this before accepting requestCrawl to confirm
	// the target host actually speaks the PDS protocol — without it every
	// crawl request is rejected with "server is not a PDS", regardless of how
	// correct each user's own did:web documents are.
	r.Get("/xrpc/com.atproto.server.describeServer", s.DescribeServer)
	// AGORA-231: the sync read surface a relay needs to backfill a repo's
	// pre-existing history, not just tail new commits off subscribeRepos.
	r.Get("/xrpc/com.atproto.sync.getRepo", s.GetRepo)
	r.Get("/xrpc/com.atproto.sync.getLatestCommit", s.GetLatestCommit)
	r.Get("/xrpc/com.atproto.sync.getBlocks", s.GetBlocks)
	// AGORA-232: how a relay learns which DIDs live on this (multi-tenant)
	// host at all, before it has any reason to call the endpoints above.
	r.Get("/xrpc/com.atproto.sync.listRepos", s.ListRepos)
	// AGORA-235: fetches a blob's actual bytes by CID — the piece a client
	// needs to render any image a record only ever references by CID
	// (avatar/banner, post images).
	r.Get("/xrpc/com.atproto.sync.getBlob", s.GetBlob)
}

// RegisterAuthedRoutes wires the endpoints only ever called by Agora's own
// authenticated frontend (AGORA-195) — resolving/following/unfollowing a
// native Bluesky account — under /api like every other frontend call, the
// same split federation.RegisterAuthedRoutes draws from federation.RegisterRoutes.
func RegisterAuthedRoutes(r chi.Router, s *Service) {
	r.Get("/atproto/lookup", s.ResolveBlueskyHandle)
	// AGORA-215: fuzzy, network-wide account search — distinct from lookup's
	// exact handle/DID resolve.
	r.Get("/atproto/search/actors", s.SearchBlueskyActors)
	// AGORA-216: read-only, on-demand network-wide post/hashtag search —
	// never ingests into local storage the way ingestAuthorFeed does.
	r.Get("/atproto/search/posts", s.SearchBlueskyPosts)
	r.Post("/atproto/follow", s.FollowBlueskyAccount)
	r.Delete("/atproto/follow/{id}", s.UnfollowBlueskyAccount)
	r.Get("/atproto/following", s.ListBlueskyFollowing)
	// AGORA-198: per-follow notification opt-in, mirroring federation's
	// /federation/follow/{id}/notify.
	r.Put("/atproto/follow/{id}/notify", s.ToggleFollowNotify)
	// AGORA-236: per-follow main-feed opt-in, mirroring federation's
	// /federation/follow/{id}/show-in-feed.
	r.Put("/atproto/follow/{id}/show-in-feed", s.ToggleShowInFeed)
	// AGORA-196: reconcile Bridgy-Fed-bridged Bluesky follows to native ones.
	r.Get("/atproto/bridged-follows", s.ListBridgedBlueskyFollows)
	r.Post("/atproto/bridged-follows/{id}/migrate", s.MigrateBridgedFollow)
}

// domainFromURL strips the scheme from an instance URL, leaving the bare
// domain. Duplicated from internal/federation/federation.go rather than
// imported — same tradeoff already made for fediverseMentionRe there: a
// three-line pure function isn't worth a cross-package dependency.
func domainFromURL(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	return strings.Split(u, "/")[0]
}

// didForUsername derives the did:web identifier for a local user, per the
// per-user-subdomain scheme (AGORA-186): did:web encodes a domain directly,
// with no path-segment colon-encoding needed since this isn't a path-based
// did:web (contrast the epic's own initial sketch, which used a path — the
// concrete infra tickets settled on subdomains instead).
func didForUsername(instanceDomain, username string) string {
	return "did:web:" + username + "." + domainFromURL(instanceDomain)
}

// atprotoEnabled is the instance-wide AT Proto kill switch (AGORA-193) —
// independent of activitypub_enabled, and checked the same "absent/unset
// means off" way federationEnabled() does (contrast activityPubEnabled's
// "absent means on", a deliberate default AGORA-156 chose to avoid yanking
// discoverability out from under instances that already had federation
// configured — not applicable here, since no instance has AT Proto
// configured yet).
func (s *Service) atprotoEnabled() bool {
	var val string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'atproto_enabled'`).Scan(&val)
	return val == "true"
}

// eligibleUser mirrors apEligibleUser's shape (internal/federation/activitypub.go)
// for the small set of columns this package needs, gated on both the
// instance-wide and per-account AT Proto toggles (AGORA-193) — every entry
// point that resolves a user's identity funnels through here or
// ensureIdentity's callers, so gating it here covers DID document /
// atproto-did resolution in one place.
type eligibleUser struct {
	ID          string
	Username    string
	AtprotoDID  string
	AtprotoPriv string
}

func (s *Service) eligibleUser(username string) (*eligibleUser, bool) {
	if !s.atprotoEnabled() {
		return nil, false
	}
	var u eligibleUser
	err := s.db.QueryRow(`
		SELECT id, username, atproto_did, atproto_private_key
		FROM users
		WHERE LOWER(username) = LOWER($1) AND is_remote = false AND profile_private = false
		  AND atproto_enabled = true AND deletion_scheduled_at IS NULL
	`, username).Scan(&u.ID, &u.Username, &u.AtprotoDID, &u.AtprotoPriv)
	if err != nil {
		return nil, false
	}
	return &u, true
}

// eligibleUserByDID is eligibleUser's counterpart for the sync read endpoints
// (AGORA-231) — a relay's getRepo/getLatestCommit/getBlocks requests identify
// the repo by "did" query param, not by subdomain, so the lookup is keyed on
// the already-persisted atproto_did column instead of re-deriving a username
// from a Host header.
func (s *Service) eligibleUserByDID(did string) (*eligibleUser, bool) {
	if !s.atprotoEnabled() || did == "" {
		return nil, false
	}
	var u eligibleUser
	err := s.db.QueryRow(`
		SELECT id, username, atproto_did, atproto_private_key
		FROM users
		WHERE atproto_did = $1 AND is_remote = false AND profile_private = false
		  AND atproto_enabled = true AND deletion_scheduled_at IS NULL
	`, did).Scan(&u.ID, &u.Username, &u.AtprotoDID, &u.AtprotoPriv)
	if err != nil {
		return nil, false
	}
	return &u, true
}

// getOrCreateSigningKey mirrors getOrCreateUserKeyPair's lazy-generation
// pattern (internal/federation/activitypub.go) — secp256k1 in place of RSA,
// hex-encoded raw key bytes in place of PEM (AT Proto keys have no PEM
// convention of their own; hex keeps the same "opaque text column" shape
// the federation_public_key/federation_private_key columns already use).
func (s *Service) getOrCreateSigningKey(userID, storedPriv string) (*atcrypto.PrivateKeyK256, error) {
	if storedPriv != "" {
		if raw, err := hex.DecodeString(storedPriv); err == nil {
			if priv, err := atcrypto.ParsePrivateBytesK256(raw); err == nil {
				return priv, nil
			}
		}
		// Fall through and regenerate if the stored key is somehow unparseable.
	}

	priv, err := atcrypto.GeneratePrivateKeyK256()
	if err != nil {
		return nil, err
	}

	if _, err := s.db.Exec(`
		UPDATE users SET atproto_private_key = $1 WHERE id = $2
	`, hex.EncodeToString(priv.Bytes()), userID); err != nil {
		return nil, err
	}

	log.Printf("atproto: generated new secp256k1 signing key for user %s", userID)
	return priv, nil
}

// resolvedIdentity is what both well-known endpoints need: the requested
// user, their DID, and their (lazily-generated, persisted) signing key.
// Extracted so DIDDocument and AtprotoDIDText — the two directions AT
// Proto's mutual handle/DID verification requires (AGORA-188) — resolve a
// request identically rather than drifting apart.
type resolvedIdentity struct {
	Username string
	DID      string
	Priv     *atcrypto.PrivateKeyK256
}

// resolveFromHost extracts the username from the request's per-user-subdomain
// Host header (AGORA-186's routing target; a spoofed Host header stands in
// for it until that infra exists), and resolves the eligible user's DID and
// signing key, persisting either if this is their first resolution.
func (s *Service) resolveFromHost(r *http.Request) (*resolvedIdentity, bool) {
	host := r.Host
	if i := strings.IndexByte(host, ':'); i != -1 {
		host = host[:i] // strip a port, if present (e.g. local dev on :8099)
	}
	username := strings.TrimSuffix(host, "."+domainFromURL(s.cfg.InstanceDomain))
	if username == "" || username == host {
		return nil, false
	}

	u, ok := s.eligibleUser(username)
	if !ok {
		return nil, false
	}

	did, priv, err := s.ensureIdentity(u.ID, u.Username, u.AtprotoDID, u.AtprotoPriv)
	if err != nil {
		return nil, false
	}

	return &resolvedIdentity{Username: u.Username, DID: did, Priv: priv}, true
}

// ensureIdentity resolves (lazily generating/persisting if needed) a user's
// DID and signing key from already-fetched column values — the userID-keyed
// counterpart to resolveFromHost's Host-header-keyed lookup, shared so
// event-triggered paths (profile sync, post federation) that already have a
// user row in hand don't need to round-trip through a fake Host header to
// reuse this logic.
func (s *Service) ensureIdentity(userID, username, storedDID, storedPriv string) (did string, priv *atcrypto.PrivateKeyK256, err error) {
	did = storedDID
	if did == "" {
		did = didForUsername(s.cfg.InstanceDomain, username)
	}
	priv, err = s.getOrCreateSigningKey(userID, storedPriv)
	if err != nil {
		return "", nil, err
	}
	if storedDID == "" {
		if _, err := s.db.Exec(`UPDATE users SET atproto_did = $1 WHERE id = $2`, did, userID); err != nil {
			return "", nil, err
		}
	}
	return did, priv, nil
}

// DIDDocument serves GET /.well-known/did.json — resolved per-hostname per
// the AT Proto handle-verification spec (AGORA-186 wires the actual
// per-user-subdomain routing this depends on; until then this is reachable
// directly by any Host header, real or spoofed in dev/test).
func (s *Service) DIDDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := s.resolveFromHost(r)
	if !ok {
		writeError(w, 404, "not found")
		return
	}

	pub, err := id.Priv.PublicKey()
	if err != nil {
		writeError(w, 500, "could not derive public key")
		return
	}
	pubK256, ok := pub.(*atcrypto.PublicKeyK256)
	if !ok {
		writeError(w, 500, "unexpected key type")
		return
	}

	keyID := id.DID + "#atproto"
	// at:// handle URI, listed in alsoKnownAs so resolution is verifiable in
	// both directions (AGORA-188): the DID document claims this handle, and
	// AtprotoDIDText independently confirms the handle resolves back to this
	// same DID — the same mutual-verification requirement WebFinger has for
	// ActivityPub actors.
	handle := id.Username + "." + domainFromURL(s.cfg.InstanceDomain)
	doc := map[string]any{
		"@context": []string{
			"https://www.w3.org/ns/did/v1",
			"https://w3id.org/security/multikey/v1",
			"https://w3id.org/security/suites/secp256k1-2019/v1",
		},
		"id":          id.DID,
		"alsoKnownAs": []string{"at://" + handle},
		"verificationMethod": []map[string]any{{
			"id":                 keyID,
			"type":               "Multikey",
			"controller":         id.DID,
			"publicKeyMultibase": pubK256.Multibase(),
		}},
		"authentication":  []string{keyID},
		"assertionMethod": []string{keyID},
		"service": []map[string]any{{
			"id":              "#atproto_pds",
			"type":            "AtprotoPersonalDataServer",
			"serviceEndpoint": strings.TrimRight(s.cfg.InstanceDomain, "/"),
		}},
	}

	w.Header().Set("Content-Type", "application/did+ld+json")
	w.WriteHeader(200)
	json.NewEncoder(w).Encode(doc)
}

// AtprotoDIDText serves GET /.well-known/atproto-did — the plain-text
// handle-to-DID resolution endpoint AT Proto clients/relays/AppViews use to
// verify a handle (AGORA-188), the counterpart to DIDDocument's alsoKnownAs
// entry. Unlike WebFinger's JRD/JSON response, this is just the bare DID
// string as the response body — no wrapper, no content negotiation.
func (s *Service) AtprotoDIDText(w http.ResponseWriter, r *http.Request) {
	id, ok := s.resolveFromHost(r)
	if !ok {
		writeError(w, 404, "not found")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(200)
	w.Write([]byte(id.DID))
}

// DescribeServer serves GET /xrpc/com.atproto.server.describeServer — the
// com.atproto.server.describeServer lexicon's minimal required shape
// (AGORA-230). This describes the instance itself, not any individual user:
// "did" is a server-level did:web for the bare instance domain, distinct
// from every per-user did:web:username.domain identity, and
// "availableUserDomains" advertises the suffix those per-user handles are
// built from.
func (s *Service) DescribeServer(w http.ResponseWriter, r *http.Request) {
	domain := domainFromURL(s.cfg.InstanceDomain)
	writeJSON(w, 200, map[string]any{
		"did":                  "did:web:" + domain,
		"availableUserDomains": []string{"." + domain},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
