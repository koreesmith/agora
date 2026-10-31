package federation

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

// ── Thread context for a remote reply (AGORA-419) ─────────────────────────────
//
// A boosted fediverse post is stored as a standalone top-level post, which is
// the only shape that can carry a repost. When the boosted post was itself a
// reply, that left it with no thread at all: Agora's comment threads only exist
// under posts this instance owns, so the conversation it belonged to was simply
// not there, and the post read like a non sequitur.
//
// Rather than graft somebody else's conversation onto Agora's two-level comment
// model, the post keeps its inReplyTo (ap_in_reply_to) and the post page asks
// for the chain above it. Each ancestor is stored the same standalone way, so
// it has a local id to open and is only fetched from its server once.

// maxAncestorHops bounds how far up a conversation one request walks. Each hop
// is at most one signed fetch to a remote server, and a page view shouldn't be
// able to set off an unbounded crawl.
const maxAncestorHops = 5

type contextPost struct {
	ID             string          `json:"id"`
	AuthorID       string          `json:"author_id"`
	Username       string          `json:"username"`
	DisplayName    string          `json:"display_name"`
	AvatarURL      string          `json:"avatar_url"`
	IsRemote       bool            `json:"is_remote"`
	RemoteInstance string          `json:"remote_instance,omitempty"`
	Content        string          `json:"content"`
	ContentWarning string          `json:"content_warning,omitempty"`
	PublishedAt    string          `json:"published_at"`
	URL            string          `json:"url,omitempty"`
	AuthorEmojis   json.RawMessage `json:"author_emojis,omitempty"`
}

// GetThreadContext returns the posts a remote reply was replying to, oldest
// first. "more" is set when the chain goes further up than could be shown
// (the hop cap, or an ancestor that couldn't be fetched or isn't public), so
// the client can offer the conversation on its own server instead; "url" is
// the post's own address there.
func (s *Service) GetThreadContext(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	empty := map[string]any{"ancestors": []contextPost{}, "more": false}

	var remotePostID, visibility, remoteInstance string
	var isRemote bool
	var parentID *string
	var inReplyTo sql.NullString
	if err := s.db.QueryRow(`
		SELECT remote_post_id, visibility, is_remote, remote_instance, parent_id, ap_in_reply_to
		FROM posts WHERE id = $1 AND deleted_at IS NULL
	`, id).Scan(&remotePostID, &visibility, &isRemote, &remoteInstance, &parentID, &inReplyTo); err != nil {
		writeJSON(w, 200, empty)
		return
	}
	// Only a public, top-level fediverse post can have a thread elsewhere.
	// Bluesky rows share is_remote but their ids are at:// URIs.
	if !isRemote || parentID != nil || visibility != "public" || !strings.HasPrefix(remotePostID, "https://") {
		writeJSON(w, 200, empty)
		return
	}

	replyTo := inReplyTo.String
	if !inReplyTo.Valid {
		// Stored before AGORA-419 recorded inReplyTo, so it isn't known yet.
		replyTo = s.learnInReplyTo(id, remotePostID)
	}

	var ids []string
	more := false
	seen := map[string]bool{remotePostID: true}
	for replyTo != "" {
		if len(ids) == maxAncestorHops || seen[replyTo] {
			more = !seen[replyTo]
			break
		}
		seen[replyTo] = true
		ancestorID, next, ok := s.resolveAncestor(replyTo)
		if !ok {
			more = true
			break
		}
		ids = append(ids, ancestorID)
		replyTo = next
	}
	if len(ids) == 0 {
		writeJSON(w, 200, map[string]any{"ancestors": []contextPost{}, "more": more, "url": remotePostID})
		return
	}

	byID := s.loadContextPosts(ids)
	ancestors := make([]contextPost, 0, len(ids))
	for i := len(ids) - 1; i >= 0; i-- {
		if p, ok := byID[ids[i]]; ok {
			ancestors = append(ancestors, p)
		}
	}
	writeJSON(w, 200, map[string]any{"ancestors": ancestors, "more": more, "url": remotePostID})
}

// learnInReplyTo fetches a stored remote post's object to find out what, if
// anything, it replies to, and records the answer so it's asked only once. A
// failed fetch records nothing, so a server that was briefly down is retried
// on the next view.
func (s *Service) learnInReplyTo(postID, objectURL string) string {
	if s.isInstanceBlocked(domainFromURL(objectURL)) {
		return ""
	}
	note, err := s.fetchRemoteNoteSignedAsInstance(objectURL)
	if err != nil || note == nil {
		return ""
	}
	s.db.Exec(`UPDATE posts SET ap_in_reply_to = $1 WHERE id = $2`, note.InReplyTo, postID)
	return note.InReplyTo
}

// resolveAncestor finds or fetches the post at objectURL and returns its local
// id and what it in turn replies to. next is "" at the top of the chain, and
// also when the ancestor is already part of a thread here: opening it lands on
// that thread, which already shows the conversation above it.
func (s *Service) resolveAncestor(objectURL string) (id, next string, ok bool) {
	// One of this instance's own posts or comments.
	if localID := localPostIDFromURL(objectURL, s.cfg.InstanceDomain); localID != "" {
		var visibility string
		if err := s.db.QueryRow(`SELECT visibility FROM posts WHERE id = $1 AND deleted_at IS NULL`, localID).
			Scan(&visibility); err != nil || visibility != "public" {
			return "", "", false
		}
		return localID, "", true
	}

	var visibility string
	var parentID *string
	var inReplyTo sql.NullString
	err := s.db.QueryRow(`
		SELECT id, visibility, parent_id, ap_in_reply_to FROM posts
		WHERE remote_post_id = $1 AND is_remote = true AND deleted_at IS NULL
	`, objectURL).Scan(&id, &visibility, &parentID, &inReplyTo)
	if err == nil {
		if visibility != "public" {
			return "", "", false
		}
		if parentID != nil {
			return id, "", true
		}
		next = inReplyTo.String
		if !inReplyTo.Valid {
			next = s.learnInReplyTo(id, objectURL)
		}
		return id, next, true
	}

	if s.isInstanceBlocked(domainFromURL(objectURL)) {
		return "", "", false
	}
	note, err := s.fetchRemoteNoteSignedAsInstance(objectURL)
	if err != nil || note == nil || note.ID == "" || note.AttributedTo == "" || !note.Public {
		return "", "", false
	}
	if s.isInstanceBlocked(domainFromURL(note.AttributedTo)) {
		return "", "", false
	}
	id, ok = s.storeStandaloneRemoteNote(note)
	return id, note.InReplyTo, ok
}

// storeStandaloneRemoteNote stores a public fediverse note as a top-level
// remote post, or finds the one already stored, and returns its id. Shared by
// a boost of a remote post (AGORA-265) and a remote reply's ancestors.
func (s *Service) storeStandaloneRemoteNote(note *apRemoteNote) (string, bool) {
	remoteAuthorID, err := s.getOrCreateRemoteAPUserAsInstance(note.AttributedTo)
	if err != nil || remoteAuthorID == "" {
		return "", false
	}

	domain := domainFromURL(note.ID)
	imageURLs, videoURL := matchAttachments(note.Attachment)
	imageAlts := attachmentAlts(note.Attachment) // AGORA-400
	var postID string
	err = s.db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, parent_id, is_remote, remote_post_id, remote_instance, content_warning, emojis, published_at, ap_in_reply_to)
		VALUES ($1, $2, 'public', NULL, true, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (remote_post_id, remote_instance) WHERE is_remote = true AND remote_post_id != '' DO NOTHING
		RETURNING id
	`, remoteAuthorID, HTMLToPlainText(note.Content), note.ID, domain, HTMLToPlainText(note.Summary), emojisJSON(emojisFromAPTags(note.Tag)), parseAPTime(note.Published), note.InReplyTo).Scan(&postID)
	if err != nil {
		// Already ingested (e.g. a previous boost of the same post, or a
		// relay forwarded it too) — look up its id so the caller still
		// attaches to it instead of dropping. A row stored before AGORA-419
		// learns its inReplyTo here for free.
		if s.db.QueryRow(`SELECT id FROM posts WHERE remote_post_id = $1 AND remote_instance = $2 AND is_remote = true`,
			note.ID, domain).Scan(&postID) != nil {
			return "", false
		}
		s.db.Exec(`UPDATE posts SET ap_in_reply_to = $1 WHERE id = $2 AND ap_in_reply_to IS NULL`, note.InReplyTo, postID)
		return postID, true
	}
	s.storeInboundImages(postID, imageURLs, imageAlts)
	s.storeInboundVideo(postID, videoURL)
	s.storeHashtags(postID, hashtagsFromAPTags(note.Tag))
	return postID, true
}

func (s *Service) loadContextPosts(ids []string) map[string]contextPost {
	out := map[string]contextPost{}
	rows, err := s.db.Query(`
		SELECT p.id, p.author_id, u.username, u.display_name, u.avatar_url, u.is_remote, u.remote_instance,
		       p.content, p.content_warning, p.published_at, p.remote_post_id, COALESCE(u.emojis::text, '{}')
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = ANY($1)
	`, pq.Array(ids))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var p contextPost
		var emojis string
		if rows.Scan(&p.ID, &p.AuthorID, &p.Username, &p.DisplayName, &p.AvatarURL, &p.IsRemote, &p.RemoteInstance,
			&p.Content, &p.ContentWarning, &p.PublishedAt, &p.URL, &emojis) == nil {
			p.AuthorEmojis = json.RawMessage(emojis)
			out[p.ID] = p
		}
	}
	return out
}
