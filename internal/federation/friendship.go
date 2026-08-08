package federation

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// Friend requests over ActivityPub (AGORA-329, ADR-002).
//
// A friend request is a Follow carrying an agora:friendRequest marker. Not an
// Offer wrapping a Relationship, despite that being the closest thing the
// ActivityStreams vocabulary has: the W3C's own primer steers away from it over
// ActivityPub because Follow is far more widely supported, and the vocabulary
// describes how to offer a relationship without ever defining how one is
// accepted. ADR-002 records the full reasoning.
//
// Riding Follow means the friendship handshake and the mutual follow that makes
// content flow are the same exchange rather than two that can drift apart, and
// it degrades correctly: a server that does not understand the marker sees a
// plain Follow and treats it as one.

// friendRequestMarker is the JSON-LD term carried on a Follow to distinguish a
// friend request from an ordinary follow. Both ends read the compact form
// directly; the @context below is what makes it a namespaced term rather than
// an invented top-level field.
const friendRequestMarker = "agora:friendRequest"

// agoraContext is the JSON-LD context every Agora-extended activity carries.
var agoraContext = []any{
	"https://www.w3.org/ns/activitystreams",
	map[string]string{"agora": "https://agora.social/ns#"},
}

// remoteAgoraActorURL returns the ActivityPub actor URL for a remote user row,
// which is what a friend request has to be addressed to.
//
// Prefers ap_actor_url when the row has one. Falls back to deriving it from
// remote_instance and remote_user_id, which is what a legacy stub carries: the
// actor URL for an Agora user is deterministic, so a friendship formed before
// this migration can still be acted on without waiting for the same person to
// be rediscovered through ActivityPub. That derivation is the bridge across the
// one-human-two-rows problem, and it only holds because the far end is Agora.
func (s *Service) remoteAgoraActorURL(userID string) (string, error) {
	var apActorURL, remoteInstance, remoteUserID string
	var isRemote bool
	err := s.db.QueryRow(`
		SELECT is_remote, COALESCE(ap_actor_url,''), COALESCE(remote_instance,''), COALESCE(remote_user_id,'')
		FROM users WHERE id = $1
	`, userID).Scan(&isRemote, &apActorURL, &remoteInstance, &remoteUserID)
	if err != nil {
		return "", fmt.Errorf("no such user")
	}
	if !isRemote {
		return "", fmt.Errorf("user is local")
	}
	if apActorURL != "" {
		return apActorURL, nil
	}
	if remoteInstance == "" || remoteUserID == "" {
		return "", fmt.Errorf("remote user has no resolvable actor")
	}
	if !isValidInstanceHost(remoteInstance) {
		return "", fmt.Errorf("invalid remote instance")
	}
	derived := "https://" + remoteInstance + "/federation/users/" + remoteUserID

	// Record the derived actor on the stub, which is what makes the reply
	// resolvable when it comes back.
	//
	// Without this the exchange is one-way: the request goes out fine, because
	// the actor URL can be derived on demand, but the Accept returns addressed
	// from an actor URL that matches no row, so the friendship stays pending
	// forever. The marked Follow that follows it then creates a *second* stub
	// and a second pending friendship, because upsertRemoteAPUser conflicts on
	// ap_actor_url and this row had none to conflict with.
	//
	// Writing it here collapses the two rows for this person into one at the
	// first moment anything needs the actor URL, which is also the first moment
	// it can be known for certain.
	s.adoptActorURLForLegacyStub(userID, derived)
	return derived, nil
}

// adoptActorURLForLegacyStub records an actor URL on a legacy stub that has
// none, so ActivityPub lookups find the same row the legacy protocol created.
//
// Guarded by the partial unique index on ap_actor_url: if a separate stub for
// the same person already claims it, this one keeps its empty value and the two
// rows stay distinct. That is the pre-existing one-human-two-rows condition and
// is not this function's to resolve; the callers that matter fall back to
// legacy identity via remoteUserIDForActor.
func (s *Service) adoptActorURLForLegacyStub(userID, actorURL string) {
	var claimedBy string
	s.db.QueryRow(`SELECT id FROM users WHERE ap_actor_url = $1`, actorURL).Scan(&claimedBy)
	if claimedBy != "" && claimedBy != userID {
		return
	}
	s.db.Exec(`UPDATE users SET ap_actor_url = $1 WHERE id = $2 AND COALESCE(ap_actor_url,'') = ''`, actorURL, userID)
}

// remoteUserIDForActor resolves an inbound actor URL to the local stub row for
// that person, trying ActivityPub identity first and legacy identity second.
//
// The fallback matters because a friendship formed under the legacy protocol
// points at a stub keyed on (remote_instance, remote_user_id) with no actor URL
// at all. adoptActorURLForLegacyStub repairs that on the outbound path, but an
// inbound activity can arrive for a stub this instance never sent anything to.
func (s *Service) remoteUserIDForActor(actorURL string) string {
	var id string
	s.db.QueryRow(`SELECT id FROM users WHERE ap_actor_url = $1`, actorURL).Scan(&id)
	if id != "" {
		return id
	}

	// Derive the legacy identity an Agora actor URL encodes:
	// https://{instance}/federation/users/{handle}
	instance := domainFromURL(actorURL)
	handle := ""
	if i := strings.LastIndex(actorURL, "/"); i >= 0 && i+1 < len(actorURL) {
		handle = actorURL[i+1:]
	}
	if instance == "" || handle == "" {
		return ""
	}
	s.db.QueryRow(`
		SELECT id FROM users
		WHERE is_remote = true AND remote_instance = $1 AND remote_user_id = $2
	`, instance, handle).Scan(&id)
	return id
}

// CanFriend reports whether a remote user can be sent a friend request.
//
// AGORA-167 refused every account with an ap_actor_url, on the correct reasoning
// that a genuine fediverse actor has no concept of friending and the request
// would sit pending forever. AGORA-329 makes that too broad: an Agora user on
// another instance now has an actor URL too, and is exactly who this is for.
//
// The distinction is whether the far end is Agora. A legacy stub always is,
// since only the Agora-to-Agora protocol creates one. Otherwise the domain has
// to be a known, unblocked peer. That is deliberately conservative: it will
// refuse an Agora instance nobody has peered with or heard from, which is a
// worse error message than it is a broken feature, where wrongly allowing it
// produces a request that silently never resolves.
func (s *Service) CanFriend(remoteUserID string) bool {
	var isRemote bool
	var apActorURL, remoteInstance, remoteUserHandle string
	err := s.db.QueryRow(`
		SELECT is_remote, COALESCE(ap_actor_url,''), COALESCE(remote_instance,''), COALESCE(remote_user_id,'')
		FROM users WHERE id = $1
	`, remoteUserID).Scan(&isRemote, &apActorURL, &remoteInstance, &remoteUserHandle)
	if err != nil {
		return false
	}
	if !isRemote {
		return true // a local account, not this function's business
	}
	if remoteUserHandle != "" {
		return true // legacy stub: only Agora creates these
	}
	if apActorURL == "" || remoteInstance == "" {
		return false
	}
	if s.isInstanceBlocked(remoteInstance) {
		return false
	}
	var known bool
	s.db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM federated_instances
		              WHERE LOWER(domain) = LOWER($1) AND status != 'blocked')
	`, strings.ToLower(remoteInstance)).Scan(&known)
	return known
}

// SendFriendRequest delivers a friend request from a local user to a remote
// Agora user, as a Follow carrying the friend-request marker.
//
// Called by internal/friends after the local pending row exists, so a delivery
// failure leaves the sender's own view intact and the queue retries.
func (s *Service) SendFriendRequest(localUserID, addresseeUserID string) {
	s.deliverFriendActivity(localUserID, addresseeUserID, "Follow", "friend request")
}

// SendFriendAccept confirms an inbound friend request. It sends two activities:
// an Accept of the original Follow, and a marked Follow back, which is what
// makes the relationship mutual and starts content flowing in both directions.
func (s *Service) SendFriendAccept(localUserID, requesterUserID string) {
	actorURL, err := s.remoteAgoraActorURL(requesterUserID)
	if err != nil {
		log.Printf("federation: friend accept: cannot resolve actor for %s: %v", requesterUserID, err)
		return
	}
	local, ok := s.localActorFor(localUserID)
	if !ok {
		return
	}
	inbox, err := s.remoteInboxFor(localUserID, actorURL)
	if err != nil {
		log.Printf("federation: friend accept: cannot resolve inbox for %s: %v", actorURL, err)
		return
	}

	// Accept the Follow they sent us.
	s.enqueueAPDelivery(localUserID, inbox, map[string]any{
		"@context": agoraContext,
		"id":       local + fmt.Sprintf("/accepts/%d", time.Now().UnixNano()),
		"type":     "Accept",
		"actor":    local,
		"object": map[string]any{
			"type":              "Follow",
			"actor":             actorURL,
			"object":            local,
			friendRequestMarker: true,
		},
	})

	// Follow them back, marked, so both sides end up with a friendship rather
	// than one friendship and one follow.
	s.deliverFriendActivity(localUserID, requesterUserID, "Follow", "friend accept")
}

// SendFriendUndo withdraws or ends a friendship: an Undo of the marked Follow.
// Covers unfriending and withdrawing a request that was never answered.
func (s *Service) SendFriendUndo(localUserID, otherUserID string) {
	actorURL, err := s.remoteAgoraActorURL(otherUserID)
	if err != nil {
		return
	}
	local, ok := s.localActorFor(localUserID)
	if !ok {
		return
	}
	inbox, err := s.remoteInboxFor(localUserID, actorURL)
	if err != nil {
		return
	}
	s.enqueueAPDelivery(localUserID, inbox, map[string]any{
		"@context": agoraContext,
		"id":       local + fmt.Sprintf("/undos/%d", time.Now().UnixNano()),
		"type":     "Undo",
		"actor":    local,
		"object": map[string]any{
			"type":              "Follow",
			"actor":             local,
			"object":            actorURL,
			friendRequestMarker: true,
		},
	})
}

// deliverFriendActivity is the shared send path for a marked Follow.
//
// Every exit is logged, including the successful one. A friend request that
// goes nowhere is otherwise indistinguishable from one that was never
// attempted, which is exactly the position AGORA-325 found the legacy queue in:
// the failure was real, silent, and only visible by querying a table by hand.
func (s *Service) deliverFriendActivity(localUserID, remoteUserID, activityType, what string) {
	actorURL, err := s.remoteAgoraActorURL(remoteUserID)
	if err != nil {
		log.Printf("federation: %s: cannot resolve actor for user %s: %v", what, remoteUserID, err)
		return
	}
	local, ok := s.localActorFor(localUserID)
	if !ok {
		// Previously a bare return. A local user who cannot be resolved to an
		// actor is a real fault (a missing or remote row), not a no-op.
		log.Printf("federation: %s: no local actor for user %s, cannot address %s", what, localUserID, actorURL)
		return
	}
	inbox, err := s.remoteInboxFor(localUserID, actorURL)
	if err != nil {
		log.Printf("federation: %s: cannot resolve inbox for %s: %v", what, actorURL, err)
		return
	}

	// enqueueAPDelivery drops silently when the recipient has blocked this
	// user (AGORA-170). That is correct behaviour and a confusing silence, so
	// say which of the two happened.
	before := s.pendingDeliveryCount(localUserID, inbox)
	s.enqueueAPDelivery(localUserID, inbox, map[string]any{
		"@context":          agoraContext,
		"id":                local + fmt.Sprintf("/follows/%d", time.Now().UnixNano()),
		"type":              activityType,
		"actor":             local,
		"object":            actorURL,
		friendRequestMarker: true,
	})
	if s.pendingDeliveryCount(localUserID, inbox) == before {
		log.Printf("federation: %s: %s -> %s was not queued (recipient has blocked this account, or the queue insert failed)", what, local, actorURL)
		return
	}
	log.Printf("federation: %s queued: %s -> %s via %s", what, local, actorURL, inbox)
}

// pendingDeliveryCount is a diagnostic helper for the logging above: it says
// whether an enqueue actually produced a row, without changing
// enqueueAPDelivery's signature, which a dozen other call sites depend on.
func (s *Service) pendingDeliveryCount(userID, inboxURL string) int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM ap_delivery_queue WHERE actor_user_id = $1 AND inbox_url = $2`,
		userID, inboxURL).Scan(&n)
	return n
}

// localActorFor resolves a local user's own actor URL.
func (s *Service) localActorFor(userID string) (string, bool) {
	var username string
	s.db.QueryRow(`SELECT username FROM users WHERE id = $1 AND is_remote = false`, userID).Scan(&username)
	if username == "" {
		return "", false
	}
	return s.actorURL(username), true
}

// remoteInboxFor resolves the inbox to deliver to, signing the lookup as the
// local user so an authorized-fetch instance does not refuse it.
func (s *Service) remoteInboxFor(localUserID, actorURL string) (string, error) {
	profile, err := s.fetchActorProfileSigned(localUserID, actorURL)
	if err != nil {
		return "", err
	}
	if profile.Inbox == "" {
		return "", fmt.Errorf("actor has no inbox")
	}
	return profile.Inbox, nil
}

// recordInboundFriendRequest creates the pending friendship an inbound marked
// Follow represents, and notifies the recipient.
//
// Mirrors handleInboundFriendRequest on the legacy path (AGORA-318) and keeps
// its two hard-won properties. The block check, because this writes a row and
// rings a bell for someone the recipient may have blocked. And the RETURNING
// gate, because a Follow is not delivered once: the sender retries on its own
// schedule and a refollow after an unfollow lands here too, so only the
// delivery that actually creates the row may notify.
//
// If the two are already friends, or a request is already pending, the insert
// no-ops and nothing is sent, which is what makes redelivery free.
func (s *Service) recordInboundFriendRequest(localUserID, remoteUserID string) {
	if s.usersHaveBlock(localUserID, remoteUserID) {
		return
	}

	// An existing friendship in either direction means this is a redelivery or
	// a crossed request, not a new one. The UNIQUE constraint is on the
	// ordered pair, so check both before inserting.
	var existing string
	s.db.QueryRow(`
		SELECT status FROM friendships
		WHERE (requester_id = $1 AND addressee_id = $2)
		   OR (requester_id = $2 AND addressee_id = $1)
	`, remoteUserID, localUserID).Scan(&existing)
	if existing != "" {
		return
	}

	var friendshipID string
	err := s.db.QueryRow(`
		INSERT INTO friendships (requester_id, addressee_id, status)
		VALUES ($1, $2, 'pending')
		ON CONFLICT DO NOTHING
		RETURNING id
	`, remoteUserID, localUserID).Scan(&friendshipID)
	if err != nil || friendshipID == "" {
		return
	}

	if s.notif != nil {
		s.notif.Create(localUserID, remoteUserID, "friend_request", "", "")
	}
}

// withdrawInboundFriendRequest removes a pending friendship the remote side has
// withdrawn, along with the notification that pointed at it (AGORA-333).
//
// Only touches a pending row. An accepted friendship being undone is
// unfriending, which is the same activity but a different local consequence,
// and is handled by the caller's own delete of the follower record; a
// friendship somebody already accepted should not silently vanish from their
// list because the other side's server sent an Undo it might be retrying.
func (s *Service) withdrawInboundFriendRequest(localUserID, remoteUserID string) {
	res, err := s.db.Exec(`
		DELETE FROM friendships
		WHERE requester_id = $1 AND addressee_id = $2 AND status = 'pending'
	`, remoteUserID, localUserID)
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return
	}
	// The notification is now pointing at nothing actionable.
	s.db.Exec(`
		DELETE FROM notifications
		WHERE user_id = $1 AND actor_id = $2 AND type = 'friend_request'
	`, localUserID, remoteUserID)
}

// handleInboundFriendAcceptAP records a remote side accepting a friend request
// this instance sent, which arrives as an Accept of the marked Follow.
func (s *Service) handleInboundFriendAcceptAP(localUserID, remoteUserID string) {
	res, err := s.db.Exec(`
		UPDATE friendships SET status = 'accepted', updated_at = NOW()
		WHERE requester_id = $1 AND addressee_id = $2 AND status = 'pending'
	`, localUserID, remoteUserID)
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return
	}
	if s.notif != nil {
		s.notif.Create(localUserID, remoteUserID, "friend_accepted", "", "")
	}
}

// friendRequestsAccepted reports whether an inbound friend request from this
// domain is allowed, per the friend_requests_from instance setting (AGORA-329).
//
// A friend request is the one inbound activity that demands a human response:
// it creates a notification and a pending row someone has to act on, which
// makes it a spam surface an ordinary follow is not. Defaults to accepting from
// anyone, matching the position everywhere else in the federation layer; the
// setting exists so an admin receiving sprayed requests has a lever short of
// blocking each source by hand.
func (s *Service) friendRequestsAccepted(domain string) bool {
	var mode string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'friend_requests_from'`).Scan(&mode)
	if mode != "peered_only" {
		return true
	}
	var peered bool
	s.db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM federated_instances
		              WHERE LOWER(domain) = LOWER($1) AND status != 'blocked')
	`, strings.ToLower(domain)).Scan(&peered)
	return peered
}
