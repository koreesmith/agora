package federation

import (
	"log"
	"strings"
	"time"
)

// Direct messages across instances (AGORA-323).
//
// Messages were local-only: internal/dm had no federation hook of any kind, and
// the legacy protocol had no message activity type. So this is a gap rather
// than a defect, and it is built on ActivityPub, which is what ADR-002 settled.
//
// The shape is the one Mastodon already uses for a direct message: a Create
// carrying a Note addressed to exactly one actor, with no Public and no
// followers collection anywhere. Nothing here is new machinery. The delivery
// queue, the per-actor keys, the HTTP Signature machinery, the inbox dispatcher
// and the blocking guards all already exist, and a side effect of using the
// standard shape is that this works with Mastodon users too, not only with
// other Agora instances.
//
// What is deliberately NOT here:
//
//   - Group conversations. conversation_participants supports more than two
//     people, but a conversation spanning three instances is a much larger
//     problem than a one-to-one, and this is scoped to one-to-one.
//   - Any form of end-to-end encryption. An ActivityPub direct message is
//     readable by both instances' administrators, and the UI says so rather
//     than implying a privacy that does not exist.
//   - Read state. Whether somebody has read a message is not something to
//     report to another server.

// dmMarker distinguishes an Agora direct message from any other directly
// addressed Note.
//
// Mastodon has no separate type for a DM either: it is a Note addressed to one
// actor and nothing else, which is also what a reply looks like before you read
// inReplyTo. The marker removes the guesswork between Agora instances, and its
// absence is handled by falling back to the same reading Mastodon uses, so a
// message from Mastodon still arrives as a message.
const dmMarker = "agora:directMessage"

// dmNotifier hands a stored inbound message back to the DM service so it
// reaches an open client the same way a local one does. Federation owns the
// wire and the storage; the socket hub belongs to internal/dm and stays there.
type dmNotifier interface {
	DeliverInboundMessage(convID, recipientID, messageID string)
}

// SetDM wires that up. Optional: an instance with no DM service still stores
// messages, they simply arrive on the next load rather than instantly.
func (s *Service) SetDM(d dmNotifier) { s.dm = d }

// SendDirectMessage delivers one message to a recipient on another instance.
//
// Called after the local row exists, like every other outbound path here, so a
// delivery failure leaves the sender's own view intact and the queue retries.
func (s *Service) SendDirectMessage(senderUserID, recipientUserID, messageID string) {
	if !s.activityPubEnabled() {
		return
	}

	var senderName string
	var apEnabled bool
	if err := s.db.QueryRow(`SELECT username, activitypub_enabled FROM users WHERE id = $1`, senderUserID).
		Scan(&senderName, &apEnabled); err != nil || !apEnabled {
		return
	}

	actorURL, err := s.remoteAgoraActorURL(recipientUserID)
	if err != nil || actorURL == "" {
		// Not somebody we can address. A local recipient never reaches here,
		// and a Bluesky account has its own messaging model entirely.
		return
	}
	inbox, err := s.remoteInboxFor(senderUserID, actorURL)
	if err != nil || inbox == "" {
		log.Printf("federation: no inbox for %s, message %s not delivered", actorURL, messageID)
		return
	}

	var content string
	var createdAt time.Time
	if err := s.db.QueryRow(`
		SELECT content, created_at FROM messages WHERE id = $1 AND author_id = $2 AND deleted_at IS NULL
	`, messageID, senderUserID).Scan(&content, &createdAt); err != nil {
		return
	}

	actor := s.actorURL(senderName)
	objID := s.dmObjectID(senderName, messageID)
	note := map[string]any{
		"id":           objID,
		"type":         "Note",
		"attributedTo": actor,
		"content":      content,
		"published":    createdAt.UTC().Format(time.RFC3339),
		"to":           []string{actorURL},
		"cc":           []string{},
		dmMarker:       true,
		// Mastodon and friends read a lone tag to render the mention, and a
		// direct message with no visible addressee reads as though it came from
		// nowhere.
		"tag": []map[string]any{{
			"type": "Mention",
			"href": actorURL,
			"name": mentionNameFor(actorURL),
		}},
	}

	create := map[string]any{
		"@context": agoraContext,
		"id":       objID + "/activity",
		"type":     "Create",
		"actor":    actor,
		"to":       []string{actorURL},
		"cc":       []string{},
		dmMarker:   true,
		"object":   note,
	}

	s.enqueueAPDelivery(senderUserID, inbox, create)
	log.Printf("federation: direct message %s queued for %s", messageID, actorURL)
}

// SendDirectMessageUpdate and SendDirectMessageDelete mirror an edit and a
// delete of a message that was already delivered.
//
// A delete that reaches fewer people than the message did is worse than no
// delete, which is the same reasoning AGORA-343 applied to limited posts, and
// for a one-to-one conversation the audience is a single actor either way.
func (s *Service) SendDirectMessageUpdate(senderUserID, recipientUserID, messageID string) {
	s.deliverDMLifecycle(senderUserID, recipientUserID, messageID, "Update")
}

func (s *Service) SendDirectMessageDelete(senderUserID, recipientUserID, messageID string) {
	s.deliverDMLifecycle(senderUserID, recipientUserID, messageID, "Delete")
}

func (s *Service) deliverDMLifecycle(senderUserID, recipientUserID, messageID, kind string) {
	if !s.activityPubEnabled() {
		return
	}

	var senderName string
	var apEnabled bool
	if err := s.db.QueryRow(`SELECT username, activitypub_enabled FROM users WHERE id = $1`, senderUserID).
		Scan(&senderName, &apEnabled); err != nil || !apEnabled {
		return
	}
	actorURL, err := s.remoteAgoraActorURL(recipientUserID)
	if err != nil || actorURL == "" {
		return
	}
	inbox, err := s.remoteInboxFor(senderUserID, actorURL)
	if err != nil || inbox == "" {
		return
	}

	actor := s.actorURL(senderName)
	objID := s.dmObjectID(senderName, messageID)

	var object map[string]any
	if kind == "Delete" {
		// Deliberately a Tombstone rather than the message's own content: a
		// delete has no business restating what it is withdrawing.
		object = map[string]any{"id": objID, "type": "Tombstone"}
	} else {
		var content string
		var createdAt time.Time
		if err := s.db.QueryRow(`
			SELECT content, created_at FROM messages WHERE id = $1 AND author_id = $2 AND deleted_at IS NULL
		`, messageID, senderUserID).Scan(&content, &createdAt); err != nil {
			return
		}
		object = map[string]any{
			"id":           objID,
			"type":         "Note",
			"attributedTo": actor,
			"content":      content,
			"published":    createdAt.UTC().Format(time.RFC3339),
			"updated":      time.Now().UTC().Format(time.RFC3339),
			"to":           []string{actorURL},
			"cc":           []string{},
			dmMarker:       true,
		}
	}

	s.enqueueAPDelivery(senderUserID, inbox, map[string]any{
		"@context": agoraContext,
		"id":       objID + "/" + strings.ToLower(kind),
		"type":     kind,
		"actor":    actor,
		"to":       []string{actorURL},
		"cc":       []string{},
		"object":   object,
	})
}

// dmObjectID is the sender's own id for a message, and the only thing an edit,
// a delete or a redelivery has to tie back to a row.
//
// Deliberately not under /posts/: a direct message is not a post, and
// localPostIDFromURL resolving one would let a message be treated as a
// federatable post target.
func (s *Service) dmObjectID(username, messageID string) string {
	return s.actorURL(username) + "/messages/" + messageID
}

// mentionNameFor renders an actor URL as the @user@host handle a mention tag
// carries. Best-effort: it is presentation, and a receiver that disagrees still
// has the href.
func mentionNameFor(actorURL string) string {
	host := domainFromURL(actorURL)
	name := actorURL
	if i := strings.LastIndex(actorURL, "/"); i >= 0 && i+1 < len(actorURL) {
		name = actorURL[i+1:]
	}
	if host == "" {
		return "@" + name
	}
	return "@" + name + "@" + host
}

// ── Inbound ───────────────────────────────────────────────────────────────────

// inboundNote is the subset of a received Note the direct-message path reads.
//
// A narrow struct rather than the full note handleInboundCreate parses, so this
// path cannot quietly start depending on a field that only makes sense for a
// post.
type inboundNote struct {
	ID            string
	Content       string
	Published     string
	InReplyTo     string
	To            []string
	CC            []string
	DirectMessage bool
	// Audience is the agora:audience marker (AGORA-337/342), read here so this
	// path can tell a friends-only or list post meant for exactly one remote
	// friend apart from an actual direct message. Both shapes are otherwise
	// identical on the wire: one actor addressed, no Public, no inReplyTo.
	Audience string
}

// handleInboundDirectMessage stores a message addressed to exactly one local
// user, applying the same policy a local message would meet.
//
// That equivalence is the whole design. dm_privacy ('everyone' / 'friends' /
// 'nobody') and the is_accepted request state already exist and already decide
// who may message whom here; a message arriving from another instance is
// checked against the same settings, so a user's answer to "who can message me"
// means the same thing whatever instance the sender is on. Inventing a separate
// federated policy would have produced two answers to one question.
//
// Returns true when the activity was a direct message, handled or refused, so
// the caller knows not to treat it as a post.
func (s *Service) handleInboundDirectMessage(verifiedActor string, note inboundNote) bool {
	if note.InReplyTo != "" || isAddressedPublicly(note.To, note.CC) {
		return false
	}
	// AGORA-337/342 give a friends-only or list post the exact same shape a
	// direct message has once its audience happens to be a single remote
	// friend: one actor addressed, no Public, no inReplyTo. That collision is
	// what sent a friends-only post to a lone remote friend into their DMs
	// instead of their wall. agora:audience is never set on an actual message,
	// so its presence settles this before the single-actor fallback below ever
	// runs.
	if note.Audience != "" {
		return false
	}
	// A Note addressed to a single actor with no inReplyTo is a direct message.
	// The marker settles it between Agora instances; without one, this is the
	// same reading Mastodon applies to its own direct messages, which is what
	// lets a message from Mastodon arrive as a message.
	if !note.DirectMessage && len(note.To) != 1 {
		return false
	}
	if s.isInstanceBlocked(domainFromURL(verifiedActor)) {
		return true
	}

	senderID := s.remoteUserIDForActor(verifiedActor)
	if senderID == "" {
		var err error
		if senderID, err = s.getOrCreateRemoteAPUser(verifiedActor, ""); err != nil || senderID == "" {
			log.Printf("federation: dropping a direct message from %s, could not resolve the sender: %v", verifiedActor, err)
			return true
		}
	}

	for _, target := range append(append([]string{}, note.To...), note.CC...) {
		username := usernameFromActorURL(target, s.cfg.InstanceDomain)
		if username == "" {
			continue
		}
		var recipientID, dmPrivacy string
		s.db.QueryRow(`
			SELECT id, COALESCE(dm_privacy, 'everyone') FROM users
			WHERE LOWER(username) = LOWER($1) AND is_remote = false AND deletion_scheduled_at IS NULL
		`, username).Scan(&recipientID, &dmPrivacy)
		if recipientID == "" {
			continue
		}
		s.storeInboundDirectMessage(senderID, recipientID, dmPrivacy, note)
	}
	return true
}

func (s *Service) storeInboundDirectMessage(senderID, recipientID, dmPrivacy string, note inboundNote) {
	if dmPrivacy == "nobody" {
		log.Printf("federation: refusing a direct message for a user who accepts none")
		return
	}
	if s.usersHaveBlock(senderID, recipientID) {
		return
	}
	isFriend := s.isAcceptedFriends(senderID, recipientID)
	if dmPrivacy == "friends" && !isFriend {
		log.Printf("federation: refusing a direct message from a non-friend to a user who accepts friends only")
		return
	}

	// Redelivery is expected traffic, not an error: peers retry on their own
	// schedule. The unique index on remote_message_id is what makes the insert
	// idempotent, and a conversation is the worst place to see a message twice.
	var convID string
	s.db.QueryRow(`
		SELECT c.id FROM conversations c
		JOIN conversation_participants p1 ON p1.conversation_id = c.id AND p1.user_id = $1
		JOIN conversation_participants p2 ON p2.conversation_id = c.id AND p2.user_id = $2
	`, senderID, recipientID).Scan(&convID)

	if convID == "" {
		if err := s.db.QueryRow(`INSERT INTO conversations DEFAULT VALUES RETURNING id`).Scan(&convID); err != nil || convID == "" {
			return
		}
		// The sender's own side is accepted; the recipient's mirrors the local
		// rule, so a stranger's first message lands as a request rather than
		// straight in the inbox.
		s.db.Exec(`INSERT INTO conversation_participants (conversation_id, user_id, is_accepted) VALUES ($1,$2,true)`, convID, senderID)
		s.db.Exec(`INSERT INTO conversation_participants (conversation_id, user_id, is_accepted) VALUES ($1,$2,$3)`, convID, recipientID, isFriend)
	}

	var msgID string
	err := s.db.QueryRow(`
		INSERT INTO messages (conversation_id, author_id, content, remote_message_id, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (remote_message_id) WHERE remote_message_id != '' DO NOTHING
		RETURNING id
	`, convID, senderID, HTMLToPlainText(note.Content), note.ID, clampPublished(parseAPTime(note.Published))).Scan(&msgID)
	if err != nil || msgID == "" {
		return // redelivery
	}
	s.db.Exec(`UPDATE conversations SET updated_at = NOW() WHERE id = $1`, convID)

	// Delivered live through the DM service's own socket hub rather than as a
	// notification, because that is what a local message does. A notification
	// only for federated messages would make the same event look different
	// depending on where the sender happens to live.
	if s.dm != nil {
		s.dm.DeliverInboundMessage(convID, recipientID, msgID)
	}
	log.Printf("federation: stored a direct message from another instance in conversation %s", convID)
}

// isAcceptedFriends is the local friendship check, by user id rather than by
// actor URL, which isAcceptedFriendByActor already resolves to.
func (s *Service) isAcceptedFriends(a, b string) bool {
	var ok bool
	s.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM friendships
			WHERE status = 'accepted'
			  AND ((requester_id = $1 AND addressee_id = $2) OR (requester_id = $2 AND addressee_id = $1))
		)
	`, a, b).Scan(&ok)
	return ok
}

// handleInboundDirectMessageUpdate and ...Delete apply an edit or a withdrawal
// to a message already stored, matched on the sender's own object id and scoped
// to that sender so one instance cannot edit another's messages.
func (s *Service) handleInboundDirectMessageUpdate(verifiedActor, objectID, content string) bool {
	senderID := s.remoteUserIDForActor(verifiedActor)
	if senderID == "" || objectID == "" {
		return false
	}
	res, err := s.db.Exec(`
		UPDATE messages SET content = $1, edited_at = NOW()
		WHERE remote_message_id = $2 AND author_id = $3 AND deleted_at IS NULL
	`, HTMLToPlainText(content), objectID, senderID)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (s *Service) handleInboundDirectMessageDelete(verifiedActor, objectID string) bool {
	senderID := s.remoteUserIDForActor(verifiedActor)
	if senderID == "" || objectID == "" {
		return false
	}
	res, err := s.db.Exec(`
		UPDATE messages SET deleted_at = NOW(), content = '', image_url = ''
		WHERE remote_message_id = $1 AND author_id = $2 AND deleted_at IS NULL
	`, objectID, senderID)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}
