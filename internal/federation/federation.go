package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/internal/store"
)

type Service struct {
	db    *store.DB
	cfg   *config.Config
	notif *notifications.Service
	// AGORA-323: set via SetDM. Optional, see dmNotifier.
	dm dmNotifier
}

func NewService(db *store.DB, cfg *config.Config, notif *notifications.Service) *Service {
	return &Service{db: db, cfg: cfg, notif: notif}
}

func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/.well-known/agora-instance", s.InstanceInfo)
	// Standard ActivityPub discovery — what Mastodon/Pleroma/etc. actually query.
	r.Get("/.well-known/webfinger", s.WebFinger)
	r.Get("/.well-known/host-meta", s.HostMeta)
	r.Get("/.well-known/nodeinfo", s.NodeInfoDiscovery)
	r.Get("/nodeinfo/2.0",         s.NodeInfo)
	r.Post("/federation/inbox",          s.Inbox)
	r.Get("/federation/users/{handle}",  s.GetUser)
	r.Get("/federation/users/{handle}/outbox",    s.Outbox)
	r.Get("/federation/users/{handle}/followers", s.Followers)
	// AGORA-357: a post's own object URL (the "id" buildNoteObject mints for
	// every Create/Update/Announce) never had anything serving a GET on it.
	r.Get("/federation/users/{handle}/posts/{postID}", s.GetPost)
	// AGORA-255: FEP-044f dereferenceable quote-authorization stamp — other
	// servers fetch this to verify a quote of one of this user's posts was
	// actually granted, not just claimed by the quoting post.
	r.Get("/federation/users/{handle}/posts/{postID}/quote-authorizations/{authID}", s.GetQuoteAuthorization)
	r.Get("/federation/search",          s.Search)
	// AGORA-115: page actors — always ActivityPub JSON, no legacy-protocol
	// fallback needed since pages never had one.
	r.Get("/federation/pages/{slug}",           s.GetPageActor)
	r.Get("/federation/pages/{slug}/outbox",    s.PageOutbox)
	r.Get("/federation/pages/{slug}/followers", s.PageFollowers)
	// AGORA-219: instance-wide actor, used only for relay Follow/Announce
	// traffic — no followers endpoint, since nothing ever follows it back.
	r.Get("/federation/instance",        s.GetInstanceActor)
	r.Get("/federation/instance/outbox", s.InstanceActorOutbox)
}

// RegisterAuthedRoutes registers federation routes that require a valid Agora
// session. LookupUser (AGORA-139) is only ever called by Agora's own
// authenticated frontend (SearchPage) — requiring auth removes it as an
// anonymous-callable surface, on top of the SSRF protection fedHTTPClient's
// dialer already provides on the outbound fetch it triggers.
func RegisterAuthedRoutes(r chi.Router, s *Service) {
	r.Get("/federation/lookup", s.LookupUser) // resolve user@instance.com
	// AGORA-146: standard-AP handle/URL resolution (search), and following a
	// remote fediverse account.
	r.Get("/federation/ap-lookup",     s.APLookup)
	r.Post("/federation/follow",        s.FollowFediverseAccount)
	r.Delete("/federation/follow/{id}", s.UnfollowFediverseAccount)
	r.Get("/federation/following",      s.ListFollowing)
	// AGORA-348: self-scoped only, distinct from the public
	// /federation/users/{handle}/followers collection (totalItems only).
	r.Get("/federation/followers",      s.ListFollowers)
	r.Put("/federation/follow/{id}/notify", s.ToggleFollowNotify)
	r.Put("/federation/follow/{id}/show-in-feed", s.ToggleShowInFeed)
}

// ── Instance info (public) ────────────────────────────────────────────────────

func (s *Service) InstanceInfo(w http.ResponseWriter, r *http.Request) {
	if !s.federationEnabled() {
		writeError(w, 404, "federation not enabled")
		return
	}

	var name, description string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'instance_name'`).Scan(&name)
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'instance_description'`).Scan(&description)

	var userCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_remote = false AND is_suspended = false`).Scan(&userCount)

	// Include instance rules
	rows, _ := s.db.Query(`SELECT text FROM instance_rules ORDER BY position ASC`)
	var rules []string
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var t string
			rows.Scan(&t)
			rules = append(rules, t)
		}
	}
	if rules == nil { rules = []string{} }

	writeJSON(w, 200, map[string]any{
		"domain":      domainFromURL(s.cfg.InstanceDomain),
		"name":        name,
		"description": description,
		// AGORA-330: no public_key. It existed only for the legacy transport's
		// Ed25519 signature, and an instance-wide signing key that nothing signs
		// with is a liability rather than a leftover. Agora-to-Agora traffic is
		// ActivityPub now, authenticated by per-actor HTTP Signatures.
		"api_version": "2",
		"user_count":  userCount,
		"software":    "agora",
		"rules":       rules,
	})
}

// ── NodeInfo (AGORA-171) ───────────────────────────────────────────────────────
//
// The standard other fediverse software (Mastodon's own "About this server"
// federation panel, instance directories, block-list tooling) uses to
// discover basic facts about a remote instance — distinct from WebFinger
// (resolves a single actor) and agora-instance (Agora's own bespoke, richer
// instance-info endpoint predating this).

func (s *Service) NodeInfoDiscovery(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"links": []map[string]string{
			{
				"rel":  "http://nodeinfo.diaspora.software/ns/schema/2.0",
				"href": strings.TrimRight(s.cfg.InstanceDomain, "/") + "/nodeinfo/2.0",
			},
		},
	})
}

func (s *Service) NodeInfo(w http.ResponseWriter, r *http.Request) {
	var userCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_remote = false AND is_suspended = false`).Scan(&userCount)

	writeJSON(w, 200, map[string]any{
		"version": "2.0",
		"software": map[string]string{
			"name":    "agora",
			"version": "2.0.0",
		},
		"protocols": []string{"activitypub"},
		"services": map[string][]string{
			"inbound":  {},
			"outbound": {},
		},
		// Agora has no registration-closed/invite-only setting today —
		// signup is always open, so this is unconditionally true rather
		// than reading a config value that doesn't exist yet.
		"openRegistrations": true,
		"usage": map[string]any{
			"users": map[string]int{"total": userCount},
		},
		"metadata": map[string]any{},
	})
}

// ── Inbox (receives activities from remote instances) ─────────────────────────

// Inbox is the shared inbox. Since AGORA-330 it speaks one protocol.
//
// It used to read the body, probe it, and branch: an activity with a @context
// went to ActivityPub, anything else was treated as a legacy Agora-to-Agora
// activity and verified against an instance-wide Ed25519 key. That second path
// and everything under it is gone, so a legacy activity now falls to
// handleStandardInbox, which refuses it as the unrecognised JSON it is. That is
// the intended outcome: a clean rejection rather than a partial understanding.
func (s *Service) Inbox(w http.ResponseWriter, r *http.Request) {
	if !s.activityPubEnabled() {
		writeError(w, 404, "federation not enabled")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, 400, "could not read body")
		return
	}

	s.handleStandardInbox(w, r, body)
}

// ── Peering (AGORA-314/321) ───────────────────────────────────────────────────

// notifyAdminsOfFederationRequest tells this instance's admins that another
// Agora instance has made contact, so an instance could not start exchanging
// posts and friend requests with no one
// noticing. This does not gate anything: the accepted position is to stay open
// and ban bad actors after the fact, and this makes the event visible so an
// admin can make that call.
//
// Filters on role = 'admin', deliberately narrower than moderation's own
// notifyAdmins (which includes moderators): the federation instance routes are
// behind RequireAdmin, so a notified moderator would have no way to act.
func (s *Service) notifyAdminsOfFederationRequest(domain string) {
	if s.notif == nil { return }

	rows, err := s.db.Query(`SELECT id FROM users WHERE role = 'admin' AND deletion_scheduled_at IS NULL`)
	if err != nil { return }
	defer rows.Close()

	var adminIDs []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		adminIDs = append(adminIDs, id)
	}
	rows.Close()

	log.Printf("federation: new instance %s federated with us, notifying %d admin(s)", domain, len(adminIDs))
	for _, id := range adminIDs {
		// No actor: this is an instance, not a user. The domain rides in the
		// data column, the shape AGORA-287's custom_domain_* types set and
		// new_report already uses for its report id.
		s.notif.Create(id, "", "federation_request", "", domain)
	}
}

// usersHaveBlock reports whether either user has blocked the other, matching
// the symmetric check friends.SendRequest performs before creating a
// friendship.
func (s *Service) usersHaveBlock(a, b string) bool {
	var blocked bool
	s.db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM blocks
		              WHERE (blocker_id = $1 AND blocked_id = $2)
		                 OR (blocker_id = $2 AND blocked_id = $1))
	`, a, b).Scan(&blocked)
	return blocked
}

// remoteAbsoluteURL resolves a possibly-relative media path against the remote
// instance that sent it. Mirrors Service.absoluteURL, which does the same job
// for this instance's own URLs, but takes the origin domain as an argument
// because here it is somebody else's.
//
// Leaves an empty value empty rather than turning it into a bare domain: an
// account with no avatar must stay that way, not acquire a broken one.
func remoteAbsoluteURL(u, instance string) string {
	if u == "" || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	if !isValidInstanceHost(instance) {
		return u
	}
	return "https://" + instance + "/" + strings.TrimLeft(u, "/")
}

// ── Remote user lookup + sync ─────────────────────────────────────────────────

// fetchRemoteProfile GETs /federation/users/{handle} on the remote instance.
// Returns an empty map on any error (caller must handle gracefully).
func (s *Service) fetchRemoteProfile(handle, instance string) map[string]string {
	if !isValidInstanceHost(instance) {
		return map[string]string{}
	}
	reqURL := "https://" + instance + "/federation/users/" + url.PathEscape(handle)
	resp, err := fedHTTPClient.Get(reqURL)
	if err != nil || resp.StatusCode != 200 {
		return map[string]string{}
	}
	defer resp.Body.Close()

	var profile map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return map[string]string{}
	}
	return profile
}

// syncStaleRemoteUsers re-fetches profiles for remote users not synced in 24h.
func (s *Service) syncStaleRemoteUsers() {
	// AGORA-330: scoped to legacy stubs. fetchRemoteProfile reads Agora's own
	// /federation/users endpoint keyed on remote_user_id, which an ActivityPub
	// row does not have, so those rows could never sync here and only consumed
	// the batch limit that the rows which can sync were competing for. They are
	// refreshed by their own actor fetch instead.
	rows, err := s.db.Query(`
		SELECT remote_user_id, remote_instance
		FROM users
		WHERE is_remote = true
		  AND COALESCE(remote_user_id, '') != ''
		  AND COALESCE(remote_instance, '') != ''
		  AND (remote_synced_at IS NULL OR remote_synced_at < NOW() - INTERVAL '24 hours')
		LIMIT 50
	`)
	if err != nil { return }
	defer rows.Close()

	type entry struct{ handle, instance string }
	var stale []entry
	for rows.Next() {
		var e entry
		rows.Scan(&e.handle, &e.instance)
		stale = append(stale, e)
	}
	rows.Close()

	for _, e := range stale {
		profile := s.fetchRemoteProfile(e.handle, e.instance)
		if len(profile) == 0 { continue }
		// AGORA-331: same guard as getOrCreateRemoteUser and
		// handleInboundProfileUpdate. This is also the path that repairs an
		// already-corrupted stub on its own, since it refetches from GetUser.
		s.db.Exec(`
			UPDATE users SET display_name = $1, avatar_url = $2, bio = $3, remote_synced_at = NOW()
			WHERE remote_user_id = $4 AND remote_instance = $5
		`, profile["display_name"], remoteAbsoluteURL(profile["avatar_url"], e.instance), profile["bio"], e.handle, e.instance)
	}
}

// ── Federated user profile ────────────────────────────────────────────────────

func (s *Service) GetUser(w http.ResponseWriter, r *http.Request) {
	handle := chi.URLParam(r, "handle")

	// Standard ActivityPub actor document — legacy flat-JSON response below is
	// unchanged for the custom protocol's own requests (no Accept header, or
	// a plain application/json Accept).
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/activity+json") || strings.Contains(accept, "application/ld+json") {
		s.writeActorObject(w, handle)
		return
	}

	var u struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		Bio         string `json:"bio"`
	}
	err := s.db.QueryRow(`
		SELECT username, display_name, avatar_url, bio
		FROM users WHERE username = $1 AND is_remote = false AND profile_private = false
	`, handle).Scan(&u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio)
	if err != nil {
		writeError(w, 404, "user not found or profile is private")
		return
	}
	u.AvatarURL = s.absoluteURL(u.AvatarURL)
	writeJSON(w, 200, u)
}

// ── Federated search ──────────────────────────────────────────────────────────

func (s *Service) Search(w http.ResponseWriter, r *http.Request) {
	if !s.federationEnabled() {
		writeError(w, 404, "federation not enabled")
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, 400, "q required")
		return
	}

	rows, err := s.db.Query(`
		SELECT username, display_name, avatar_url
		FROM users
		WHERE is_remote = false AND profile_private = false
		  AND (username ILIKE '%'||$1||'%' OR display_name ILIKE '%'||$1||'%')
		  AND deletion_scheduled_at IS NULL
		LIMIT 20
	`, q)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	type User struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		Instance    string `json:"instance"`
	}
	domain := domainFromURL(s.cfg.InstanceDomain)
	var users []User
	for rows.Next() {
		var u User
		rows.Scan(&u.Username, &u.DisplayName, &u.AvatarURL)
		u.Instance = domain
		users = append(users, u)
	}
	if users == nil { users = []User{} }
	writeJSON(w, 200, map[string]any{"users": users})
}

// ── Cross-instance user lookup ────────────────────────────────────────────────

// LookupUser resolves a user@instance handle by fetching their profile from the
// remote instance and creating/updating the local stub. Returns the local profile.
// Query param: handle=username@instance.com
func (s *Service) LookupUser(w http.ResponseWriter, r *http.Request) {
	if !s.federationEnabled() {
		writeError(w, 404, "federation not enabled")
		return
	}

	raw := r.URL.Query().Get("handle")
	if raw == "" {
		writeError(w, 400, "handle required — format: username@instance.com")
		return
	}

	parts := strings.SplitN(raw, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		writeError(w, 400, "invalid handle — format: username@instance.com")
		return
	}
	username, instance := parts[0], parts[1]

	if !isValidInstanceHost(instance) {
		writeError(w, 400, "invalid instance domain")
		return
	}

	// Don't look up our own users this way
	localDomain := domainFromURL(s.cfg.InstanceDomain)
	if instance == localDomain {
		var u struct {
			Username    string `json:"username"`
			DisplayName string `json:"display_name"`
			AvatarURL   string `json:"avatar_url"`
			Bio         string `json:"bio"`
			ID          string `json:"id"`
			IsRemote    bool   `json:"is_remote"`
		}
		s.db.QueryRow(`SELECT id, username, display_name, avatar_url, bio FROM users WHERE username = $1 AND is_remote = false AND deletion_scheduled_at IS NULL`, username).
			Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio)
		if u.ID == "" {
			writeError(w, 404, "user not found")
			return
		}
		writeJSON(w, 200, map[string]any{"user": u, "local": true})
		return
	}

	// AGORA-330: resolved through ActivityPub, not through the legacy protocol's
	// own profile endpoint.
	//
	// This used to call getOrCreateRemoteUser, which keyed a stub on
	// (remote_user_id, remote_instance) and gave it no actor URL. Every other
	// path already keyed the same person on ap_actor_url, so looking somebody up
	// here and then meeting them over ActivityPub produced two rows for one
	// human, with the friendship on one and the posts on the other. That is the
	// duplication ADR-002 set out to end, and this was its last source.
	//
	// WebFinger then the actor document is the same route APLookup takes, so a
	// handle resolves to one identity however it was reached.
	actorURL, err := resolveActorURLViaWebFinger(username, instance)
	if err != nil {
		writeError(w, 404, "user not found on remote instance, check the handle and try again")
		return
	}
	localID, err := s.getOrCreateRemoteAPUser(actorURL, auth.UserIDFromCtx(r.Context()))
	if err != nil || localID == "" {
		log.Printf("federation: lookup of %s@%s resolved to %s but the actor fetch failed: %v", username, instance, actorURL, err)
		writeError(w, 404, "user not found on remote instance, check the handle and try again")
		return
	}

	type Result struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		Bio         string `json:"bio"`
		IsRemote    bool   `json:"is_remote"`
		Instance    string `json:"remote_instance"`
	}
	var u Result
	s.db.QueryRow(`SELECT id, username, display_name, avatar_url, bio, is_remote, remote_instance FROM users WHERE id = $1`, localID).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio, &u.IsRemote, &u.Instance)

	writeJSON(w, 200, map[string]any{"user": u, "local": false})
}

func (s *Service) FetchInstanceInfo(domain string) (string, string, string, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	domain = strings.TrimPrefix(strings.TrimPrefix(domain, "https://"), "http://")
	domain = strings.Split(domain, "/")[0]

	if !isValidInstanceHost(domain) {
		return "", "", "", fmt.Errorf("invalid instance domain")
	}

	resp, err := fedHTTPClient.Get("https://" + domain + "/.well-known/agora-instance")
	if err != nil { return "", "", "", fmt.Errorf("could not reach instance: %w", err) }
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", "", fmt.Errorf("instance returned %d", resp.StatusCode)
	}

	var info struct {
		Name     string `json:"name"`
		Software string `json:"software"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", "", "", fmt.Errorf("invalid instance info")
	}

	// AGORA-330: the public key this used to demand and validate went with the
	// legacy transport. An instance on this build publishes none, so continuing
	// to require one would refuse to peer with exactly the instances worth
	// peering with. The third return stays for the callers' signature and is
	// always empty; peering no longer turns on a key.
	//
	// Answering this endpoint at all is the identification: only Agora serves
	// it. The software field is checked anyway, since a proxy or a parked domain
	// returning some other 200 JSON should not become a peer.
	if info.Software != "" && !strings.EqualFold(info.Software, "agora") {
		return "", "", "", fmt.Errorf("%s is not an Agora instance", domain)
	}

	return domain, info.Name, "", nil
}

// ── Inbound peering (AGORA-314/321, rehomed by AGORA-330) ─────────────────────

// registerInboundPeer records another Agora instance that has made contact, and
// notifies this instance's admins the first time it does.
//
// This used to happen inside getRemotePublicKey, as a side effect of fetching a
// peer's signing key to verify a legacy activity. Deleting that transport would
// have quietly taken first-contact registration with it, and with it the
// Federation tab's inbound direction, the admin notification, and the peered
// check that CanFriend and friend_requests_from both read. So it moves here and
// becomes deliberate rather than incidental.
//
// Called from the Agora-marked ActivityPub activities: a friend request or a
// limited-audience post. Those are precisely the traffic where a peering
// relationship means something, and gating on them keeps every Mastodon server
// that ever delivers to us out of a tab that means "Agora instances we federate
// with". An existing row short-circuits before any network call, so the probe
// below happens at most once per new peer.
func (s *Service) registerInboundPeer(domain string) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || domain == domainFromURL(s.cfg.InstanceDomain) || s.isInstanceBlocked(domain) {
		return
	}

	var exists bool
	s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM federated_instances WHERE LOWER(domain) = $1)`, domain).Scan(&exists)
	if exists {
		s.db.Exec(`UPDATE federated_instances SET last_seen_at = NOW() WHERE LOWER(domain) = $1`, domain)
		return
	}

	normalized, name, _, err := s.FetchInstanceInfo(domain)
	if err != nil {
		// Not reachable, or not Agora. Either way it does not belong in the
		// peer list, and the activity itself is unaffected: HTTP Signatures
		// already authenticated it, and peering is not an authorisation gate.
		return
	}

	// RETURNING (xmax = 0) is what tells a genuine insert from the update
	// branch, so a burst of activities from the same new instance produces one
	// notification rather than one per activity.
	//
	// An existing 'outbound' peering becomes 'mutual' once they contact us.
	// 'unknown' deliberately stays 'unknown': inbound traffic proves they are
	// talking to us, not that they initiated the peering, and guessing would
	// turn every pre-existing row into a false "they connected to you".
	var firstContact bool
	s.db.QueryRow(`
		INSERT INTO federated_instances (domain, name, instance_url, status, direction)
		VALUES ($1, $2, $3, 'active', 'inbound')
		ON CONFLICT (domain) DO UPDATE
		  SET name         = $2,
		      last_seen_at = NOW(),
		      direction    = CASE WHEN federated_instances.direction = 'outbound' THEN 'mutual'
		                          ELSE federated_instances.direction END
		RETURNING (xmax = 0)
	`, normalized, name, "https://"+normalized).Scan(&firstContact)

	if firstContact {
		go s.notifyAdminsOfFederationRequest(normalized)
	}
}

// ── Background sync ───────────────────────────────────────────────────────────

func (s *Service) StartBackgroundSync(ctx context.Context) {
	// Deliberately does NOT gate the whole loop on federationEnabled() at
	// startup — that was a bug: federation_enabled is an admin-toggleable
	// runtime setting, but a one-time check here meant that if it happened to
	// be off (or unset) at the exact moment the server process started, the
	// delivery-queue drain loop would never run again for that process's
	// lifetime, even after an admin turned federation back on — outbound
	// activities would sit queued forever until the next restart. Instead the
	// loop always runs, and each tick re-checks the current value.
	//
	// AGORA-330: the legacy queue's own ticker went with the transport. What is
	// left on federationEnabled is the Agora-native surface that outlived it:
	// the peer liveness refresh and the remote-profile sync.
	apQueueTicker := time.NewTicker(20 * time.Second) // drain standard-AP delivery queue
	syncTicker   := time.NewTicker(15 * time.Minute)  // refresh instance list
	profileTicker := time.NewTicker(6 * time.Hour)    // sync stale remote profiles

	defer apQueueTicker.Stop()
	defer syncTicker.Stop()
	defer profileTicker.Stop()

	// Run immediately on start. The delivery queues are gated by
	// activityPubEnabled (AGORA-156); peering upkeep stays on
	// federationEnabled, which is still the Agora-native surface's toggle.
	if s.activityPubEnabled() {
		go s.drainAPQueue()
		go s.drainPageAPQueue()
		go s.drainInstanceAPQueue() // AGORA-220
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-apQueueTicker.C:
			if s.activityPubEnabled() {
				go s.drainAPQueue()
				go s.drainPageAPQueue()
				go s.drainInstanceAPQueue() // AGORA-220
			}
		case <-syncTicker.C:
			if s.federationEnabled() {
				go s.refreshInstances()
			}
		case <-profileTicker.C:
			if s.federationEnabled() {
				go s.syncStaleRemoteUsers()
			}
		}
	}
}

func (s *Service) refreshInstances() {
	rows, _ := s.db.Query(`SELECT domain FROM federated_instances WHERE status = 'active'`)
	if rows == nil { return }
	defer rows.Close()
	for rows.Next() {
		var domain string
		rows.Scan(&domain)
		go func(d string) {
			if !isValidInstanceHost(d) { return }
			resp, err := fedHTTPClient.Get("https://" + d + "/.well-known/agora-instance")
			if err != nil { return }
			resp.Body.Close()
			if resp.StatusCode == 200 {
				s.db.Exec(`UPDATE federated_instances SET last_seen_at = NOW() WHERE domain = $1`, d)
			}
		}(domain)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (s *Service) federationEnabled() bool {
	var val string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'federation_enabled'`).Scan(&val)
	return val == "true"
}

// activityPubEnabled is the instance-wide toggle for standard ActivityPub
// (AGORA-156) — distinct from federationEnabled, which gates the older
// custom Agora-to-Agora protocol. Defaults to on (unset != "false") so
// existing instances that already have federation configured don't lose
// fediverse discoverability the moment this ships; an admin can turn it off
// explicitly in Admin > Settings.
func (s *Service) activityPubEnabled() bool {
	var val string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'activitypub_enabled'`).Scan(&val)
	return val != "false"
}

// isInstanceBlocked is the single enforcement point for "is this fediverse
// instance blocked" (AGORA-177), checked instead of the ad-hoc
// federated_instances.status queries that used to be scattered across this
// file and activitypub.go. It's true if EITHER an admin has explicitly
// banned the domain via Admin > Moderation (instance_bans — previously
// unenforced anywhere) OR the domain's federated_instances row (the legacy
// Agora-to-Agora protocol's known-instances list, opportunistically
// populated by getRemotePublicKey/verifyActivity) has been marked blocked
// via Admin > Federation. Either UI can block a domain outright — neither
// requires the other's row to exist first.
func (s *Service) isInstanceBlocked(domain string) bool {
	domain = strings.ToLower(domain)
	var blocked bool
	s.db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM federated_instances WHERE domain = $1 AND status = 'blocked')
		    OR EXISTS(SELECT 1 FROM instance_bans WHERE LOWER(instance) = $1)
	`, domain).Scan(&blocked)
	return blocked
}

func domainFromURL(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	return strings.Split(u, "/")[0]
}

// ── SSRF protection ─────────────────────────────────────────────────────────────
//
// All outbound federation requests go through fedHTTPClient, whose dialer refuses
// to connect to non-public IP addresses. This prevents an attacker-supplied
// instance host (e.g. via /federation/lookup) from making the server reach
// internal services, cloud metadata endpoints (169.254.169.254), or loopback.

var fedHTTPClient = &http.Client{
	Timeout:   10 * time.Second,
	Transport: &http.Transport{DialContext: safeDialContext},
}

// isPublicIP reports whether ip is a globally routable address we're willing to
// connect to. Loopback, private, link-local, CGNAT, unspecified, and multicast
// ranges are all rejected.
func isPublicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// 100.64.0.0/10 — carrier-grade NAT (not covered by IsPrivate)
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return false
	}
	return true
}

// safeDialContext resolves the target host, verifies every resolved IP is public,
// then dials a validated IP directly (closing the DNS-rebinding window between
// the check and the connection).
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, ipa := range ips {
		if !isPublicIP(ipa.IP) {
			return nil, fmt.Errorf("refusing to connect to non-public address %s", ipa.IP)
		}
	}
	d := &net.Dialer{Timeout: 10 * time.Second}
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}

// isValidInstanceHost performs cheap syntactic validation on a federation host
// before it's ever placed into a URL. It rejects empty values, over-long names,
// and anything containing characters that could alter the request target.
func isValidInstanceHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	if strings.ContainsAny(h, "/\\?#@ \t\r\n") {
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
