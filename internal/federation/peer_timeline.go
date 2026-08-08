package federation

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// Peer timeline exchange (AGORA-322).
//
// An Agora instance can act as a lightweight relay for a peer: with the toggle
// on, that peer's public posts appear locally without any local user having to
// follow anyone there.
//
// This is the cold-start problem. A new or small instance has a sparse Explore
// tab until its users go and find people, and the only bulk alternative is
// joining a public relay, which brings the entire fediverse firehose with it.
// Peering with one known, admin-vetted instance is a far more precise
// instrument. It also gives peering an observable purpose: before this, an
// admin added an instance and nothing happened.
//
// Two decisions worth keeping in view:
//
//   - It lands in Explore, never the home feed. The home feed is friends and
//     follows, and nothing an admin does should put content there. Explore
//     already carries remote posts from any account this instance knows about,
//     so peer content there is consistent with what that surface already means
//     rather than a new kind of thing.
//   - The two directions are independent. Our pulling a peer's timeline says
//     nothing about whether they want ours, and vice versa. Conflating them
//     would let one instance's admin silently change another's outbound volume.

// ── Receiving a peer's timeline ───────────────────────────────────────────────

// SetTimelineExchange turns the subscription to a peer's timeline on or off.
//
// Turning it on sends a Follow from this instance's actor, reusing the relay
// handshake's shape, since that is exactly what this is: a subscription to
// everything public an instance has, rather than to one account.
func (s *Service) SetTimelineExchange(domain string, on bool) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return fmt.Errorf("no domain")
	}
	if on && s.isInstanceBlocked(domain) {
		return fmt.Errorf("instance is blocked")
	}

	res, err := s.db.Exec(`UPDATE federated_instances SET timeline_exchange = $1 WHERE LOWER(domain) = $2`, on, domain)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("not a peer")
	}

	inbox := "https://" + domain + "/federation/inbox"
	if on {
		s.sendRelayFollow(inbox)
		log.Printf("federation: subscribed to %s's timeline", domain)
	} else {
		s.sendRelayUndo(inbox)
		log.Printf("federation: unsubscribed from %s's timeline", domain)
	}
	return nil
}

// isTimelineExchangePeer reports whether an inbound activity's signer is a peer
// whose timeline this instance subscribes to.
//
// This is what lets their Creates take the relay ingestion path, which does not
// require a local follower. Scoped to peers the toggle is actually on for, so
// an ordinary peering does not quietly start pulling content.
func (s *Service) isTimelineExchangePeer(actorURL string) bool {
	domain := domainFromURL(actorURL)
	if domain == "" || s.isInstanceBlocked(domain) {
		return false
	}
	var on bool
	s.db.QueryRow(`
		SELECT COALESCE(timeline_exchange, false) FROM federated_instances
		WHERE LOWER(domain) = LOWER($1) AND status != 'blocked'
	`, domain).Scan(&on)
	return on
}

// ── Serving our timeline to a peer ────────────────────────────────────────────

// handleInboundFollowInstance answers a Follow of this instance's own actor,
// which is an instance asking to carry our public posts.
//
// Accepted from any instance that is not blocked. Public posts are public, and
// anyone can already read them through a user's outbox, so there is no privacy
// line here and isInstanceBlocked remains the enforcement point. What this does
// add is outbound delivery volume, which is exactly why it belongs in the
// Federation tab where an admin can see it.
func (s *Service) handleInboundFollowInstance(followID, followerActor string) {
	domain := domainFromURL(followerActor)
	if domain == "" || s.isInstanceBlocked(domain) {
		return
	}

	// An instance subscribing to us is "they connected to you", so it goes
	// through the same first-contact path as any other inbound peering rather
	// than into a separate, invisible subscriber list (AGORA-314/321).
	s.registerInboundPeer(domain)

	res, err := s.db.Exec(`
		UPDATE federated_instances SET carries_our_timeline = true, last_seen_at = NOW()
		WHERE LOWER(domain) = LOWER($1) AND status != 'blocked'
	`, domain)
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// registerInboundPeer declined, which means the far end did not answer
		// as an Agora instance. Nothing to serve a timeline to.
		log.Printf("federation: %s asked to carry our timeline but is not a recognised peer", domain)
		return
	}

	actor := s.instanceActorURL()
	followObj := map[string]any{"type": "Follow", "actor": followerActor, "object": actor}
	if followID != "" {
		followObj["id"] = followID
	}
	s.enqueueInstanceAPDelivery("https://"+domain+"/federation/inbox", map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       actor + fmt.Sprintf("/accepts/%d", time.Now().UnixNano()),
		"type":     "Accept",
		"actor":    actor,
		"object":   followObj,
	})
	log.Printf("federation: %s now carries this instance's public timeline", domain)
}

// handleInboundUndoFollowInstance stops serving our timeline to a peer that
// unsubscribes.
func (s *Service) handleInboundUndoFollowInstance(followerActor string) {
	domain := domainFromURL(followerActor)
	if domain == "" {
		return
	}
	s.db.Exec(`UPDATE federated_instances SET carries_our_timeline = false WHERE LOWER(domain) = LOWER($1)`, domain)
	log.Printf("federation: %s no longer carries this instance's timeline", domain)
}

// peerTimelineInboxes lists the peers subscribed to this instance's timeline.
//
// Used alongside enabledRelayInboxes at the three places a public post, edit or
// delete goes out, so a peer sees the same lifecycle a relay does. An edit or a
// delete that reaches fewer places than the post did is the failure worth
// avoiding here, which is why all three call it and not just the Create.
func (s *Service) peerTimelineInboxes() []string {
	rows, err := s.db.Query(`
		SELECT 'https://' || domain || '/federation/inbox'
		FROM federated_instances
		WHERE carries_our_timeline = true AND status != 'blocked'
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var inbox string
		if rows.Scan(&inbox) == nil && inbox != "" {
			out = append(out, inbox)
		}
	}
	return out
}
