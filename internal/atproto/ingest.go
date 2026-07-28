package atproto

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api/bsky"
)

// AGORA-197 chose AppView polling over consuming Bluesky's network-wide
// firehose. Agora's existing firehose (AGORA-191) is outbound-only — this
// instance's own commits, served to a relay that asks for them. Ingesting
// followed accounts' posts by *consuming* the wider network's firehose
// would mean processing every commit across the entire Bluesky network
// continuously (subscribeRepos has no server-side filter by DID), just to
// catch updates from what's realistically a handful of follows per
// instance — a large, ongoing operational cost (bandwidth, CBOR/MST
// parsing at network scale, cursor/backfill management) for a niche
// feature. Polling app.bsky.feed.getAuthorFeed per followed DID scales
// with follow count instead of network size, and reuses the AppView
// client AGORA-195 already built.
const authorFeedPollInterval = 5 * time.Minute
const authorFeedFetchLimit = 20

func displayNameOr(displayName, fallback string) string {
	if displayName != "" {
		return displayName
	}
	return fallback
}

// getOrCreateRemoteATUser mirrors federation's getOrCreateRemoteAPUser/
// upsertRemoteAPUser (internal/federation/activitypub.go) — a cached local
// stub `users` row for a remote account. Keyed by DID (atproto_remote_did)
// rather than handle, since a Bluesky handle can change while the DID
// never does — the same reasoning ap_actor_url uses a stable actor URI
// instead of a display name.
//
// coverURL is frequently empty: only resolveBlueskyActor's detailed profile
// fetch ever has a banner to offer (ProfileViewBasic, all this function's
// other callers have, carries no banner field at all) — an empty value here
// intentionally leaves any previously-cached cover_url alone rather than
// blanking it out, the same COALESCE-style tradeoff avatarURL doesn't need
// since every caller already has *some* avatar to offer.
func (s *Service) getOrCreateRemoteATUser(did, handle, displayName, avatarURL, coverURL string) (string, error) {
	if handle == "" {
		handle = "user"
	}
	var id string
	err := s.db.QueryRow(`
		INSERT INTO users (username, email, password_hash, display_name, avatar_url, cover_url,
		                   email_verified, is_remote, remote_instance, atproto_remote_did, profile_private)
		VALUES ($1, $1, '', $2, $3, $4, true, true, 'bsky.app', $5, false)
		ON CONFLICT (atproto_remote_did) WHERE atproto_remote_did != '' DO UPDATE
		  SET display_name = $2, avatar_url = $3, cover_url = COALESCE(NULLIF($4, ''), users.cover_url), username = $1, profile_private = false
		RETURNING id
	`, handle, displayNameOr(displayName, handle), avatarURL, coverURL, did).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// storeInboundImages mirrors federation's own helper of the same name
// (internal/federation/activitypub.go) — remote images are referenced by
// URL (the AppView's CDN, in this case), not re-downloaded/re-hosted.
func (s *Service) storeInboundImages(postID string, imageURLs []string) {
	if len(imageURLs) == 0 {
		return
	}
	s.db.Exec(`UPDATE posts SET image_url = $1 WHERE id = $2`, imageURLs[0], postID)
	if len(imageURLs) > 1 {
		for i, u := range imageURLs {
			s.db.Exec(`INSERT INTO post_photos (post_id, url, position) VALUES ($1, $2, $3)`, postID, u, i)
		}
	}
}

// storeInboundEmbed persists whatever richer content a Bluesky post's embed
// carries beyond plain text. Images were already handled (AGORA-197); a
// shared link and a picked GIF are indistinguishable at the lexicon level —
// Bluesky has no native GIF attachment type, its GIF picker just embeds a
// Tenor URL as a normal app.bsky.embed.external, same as any link card — so
// both fall out of the External case with no special-casing (AGORA-250).
//
// A quote-post embed (EmbedRecord_View, or the Record half of
// RecordWithMedia) resolves the quoted post and wires it up exactly the way
// a local quote-share already works: repost_of_id pointing at the quoted
// post, this post's own text left in content as the quoter's commentary
// (AGORA-239/247's shape, applied to Bluesky's version of the same concept).
func (s *Service) storeInboundEmbed(ctx context.Context, postID string, embed *bsky.FeedDefs_PostView_Embed) {
	if embed == nil {
		return
	}
	switch {
	case embed.EmbedImages_View != nil:
		s.storeInboundBskyImages(postID, embed.EmbedImages_View)
	case embed.EmbedVideo_View != nil:
		s.storeInboundVideo(postID, embed.EmbedVideo_View)
	case embed.EmbedExternal_View != nil:
		s.storeInboundLink(postID, embed.EmbedExternal_View)
	case embed.EmbedRecord_View != nil:
		s.storeInboundQuote(ctx, postID, embed.EmbedRecord_View)
	case embed.EmbedRecordWithMedia_View != nil:
		media := embed.EmbedRecordWithMedia_View.Media
		switch {
		case media.EmbedImages_View != nil:
			s.storeInboundBskyImages(postID, media.EmbedImages_View)
		case media.EmbedVideo_View != nil:
			s.storeInboundVideo(postID, media.EmbedVideo_View)
		case media.EmbedExternal_View != nil:
			s.storeInboundLink(postID, media.EmbedExternal_View)
		}
		s.storeInboundQuote(ctx, postID, embed.EmbedRecordWithMedia_View.Record)
	}
}

// storeInboundQuote resolves a quote-post embed's target and, if it
// actually names a post (as opposed to a blocked/deleted/detached record, or
// a quote of a feed generator/list/labeler/starter pack — none of which have
// a post to point a repost_of_id at), sets postID's repost_of_id to it.
func (s *Service) storeInboundQuote(ctx context.Context, postID string, view *bsky.EmbedRecord_View) {
	if view == nil || view.Record == nil || view.Record.EmbedRecord_ViewRecord == nil {
		return
	}
	quotedID := s.ingestQuotedPost(ctx, view.Record.EmbedRecord_ViewRecord)
	if quotedID == "" {
		return
	}
	s.db.ExecContext(ctx, `UPDATE posts SET repost_of_id = $1 WHERE id = $2`, quotedID, postID)
}

// ingestQuotedPost upserts a quoted post as a real, locally stored post —
// the same (remote_post_id, remote_instance) identity/dedup scheme every
// other inbound Bluesky post uses — and returns its local id. The
// "DO UPDATE SET remote_post_id = EXCLUDED.remote_post_id" is a no-op update
// used only to make RETURNING fire on conflict too: unlike top-level
// ingestion (which only cares that ON CONFLICT DO NOTHING means "already
// seen, nothing to do"), this needs the local id back either way, since the
// quoting post's repost_of_id has to be set whether the quoted post was
// already known or is being ingested for the first time right now.
//
// Only resolves one level deep: a quoted post's own embeds are stored (so
// its images/link/video still show), but a further nested quote-of-a-quote
// isn't chased — enough to cover the reported gap without open-ended
// recursion.
func (s *Service) ingestQuotedPost(ctx context.Context, rec *bsky.EmbedRecord_ViewRecord) string {
	if rec == nil || rec.Author == nil {
		return ""
	}
	post, ok := rec.Value.Val.(*bsky.FeedPost)
	if !ok || post == nil {
		return ""
	}
	if s.isBlueskyActorBlocked(rec.Author.Did, rec.Author.Handle) {
		return ""
	}

	var displayName, avatarURL string
	if rec.Author.DisplayName != nil {
		displayName = *rec.Author.DisplayName
	}
	if rec.Author.Avatar != nil {
		avatarURL = *rec.Author.Avatar
	}
	authorID, err := s.getOrCreateRemoteATUser(rec.Author.Did, rec.Author.Handle, displayName, avatarURL, "")
	if err != nil {
		return ""
	}

	var postID string
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO posts (author_id, content, visibility, parent_id, is_remote, remote_post_id, remote_instance, remote_post_cid, content_warning)
		VALUES ($1, $2, 'public', NULL, true, $3, 'bsky.app', $4, $5)
		ON CONFLICT (remote_post_id, remote_instance) WHERE is_remote = true AND remote_post_id != '' DO UPDATE SET remote_post_id = EXCLUDED.remote_post_id
		RETURNING id
	`, authorID, post.Text, rec.Uri, rec.Cid, contentWarningFromLabels(post.Labels)).Scan(&postID)
	if err != nil || postID == "" {
		return ""
	}

	if len(rec.Embeds) > 0 && rec.Embeds[0] != nil {
		e := rec.Embeds[0]
		switch {
		case e.EmbedImages_View != nil:
			s.storeInboundBskyImages(postID, e.EmbedImages_View)
		case e.EmbedVideo_View != nil:
			s.storeInboundVideo(postID, e.EmbedVideo_View)
		case e.EmbedExternal_View != nil:
			s.storeInboundLink(postID, e.EmbedExternal_View)
		}
	}
	return postID
}

func (s *Service) storeInboundBskyImages(postID string, imgs *bsky.EmbedImages_View) {
	var imageURLs []string
	for _, img := range imgs.Images {
		if img.Fullsize != "" {
			imageURLs = append(imageURLs, img.Fullsize)
		}
	}
	s.storeInboundImages(postID, imageURLs)
}

// storeInboundLink maps a Bluesky link/GIF card onto Agora's own link
// preview columns — the same shape a locally-created post's link preview
// already populates. Thumb/Uri are already fully-resolved CDN URLs on the
// hydrated View type the AppView returns, not blob refs needing a separate
// fetch, the same way EmbedImages_View.Fullsize already is.
func (s *Service) storeInboundLink(postID string, ext *bsky.EmbedExternal_View) {
	if ext == nil || ext.External == nil || ext.External.Uri == "" {
		return
	}
	e := ext.External
	var thumb string
	if e.Thumb != nil {
		thumb = *e.Thumb
	}
	s.db.Exec(`
		UPDATE posts SET link_url = $1, link_title = $2, link_description = $3, link_image = $4, link_domain = $5
		WHERE id = $6
	`, e.Uri, e.Title, e.Description, thumb, domainFromURL(e.Uri), postID)
}

// storeInboundVideo maps a native app.bsky.embed.video onto Agora's own
// video_url/video_thumb_url columns. Playlist is an HLS (.m3u8) stream URL
// from Bluesky's video CDN rather than a single mp4 file — playback support
// varies by client/browser, but that's still strictly better than the empty
// gap this closes.
func (s *Service) storeInboundVideo(postID string, v *bsky.EmbedVideo_View) {
	if v == nil || v.Playlist == "" {
		return
	}
	var thumb string
	if v.Thumbnail != nil {
		thumb = *v.Thumbnail
	}
	s.db.Exec(`UPDATE posts SET video_url = $1, video_thumb_url = $2 WHERE id = $3`, v.Playlist, thumb, postID)
}

// storeHashtagsFromFacets (AGORA-213) mirrors federation's storeHashtags/
// hashtagsFromAPTags pair, but reads an AT Proto record's own "facets"
// array directly — a #tag facet's Tag field is already bare (no leading #
// per the app.bsky.richtext.facet#tag lexicon), so this only needs to
// lowercase and dedupe, not strip a prefix the way the Fediverse Hashtag
// tag-name parsing does.
func (s *Service) storeHashtagsFromFacets(postID string, facets []*bsky.RichtextFacet) {
	seen := map[string]bool{}
	var tags []string
	for _, f := range facets {
		if f == nil {
			continue
		}
		for _, feat := range f.Features {
			if feat == nil || feat.RichtextFacet_Tag == nil || feat.RichtextFacet_Tag.Tag == "" {
				continue
			}
			tag := strings.ToLower(feat.RichtextFacet_Tag.Tag)
			if seen[tag] {
				continue
			}
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	s.db.Exec(`DELETE FROM post_hashtags WHERE post_id = $1`, postID)
	for _, tag := range tags {
		s.db.Exec(`INSERT INTO post_hashtags (post_id, tag) VALUES ($1, $2) ON CONFLICT DO NOTHING`, postID, tag)
	}
}

// ingestAuthorFeed fetches a followed DID's recent posts from the AppView
// and ingests any not already seen. Idempotent via the same
// (remote_post_id, remote_instance) unique index AGORA-146's fediverse
// ingestion already relies on for redelivery safety — an AT-URI is a stable,
// globally unique post identifier, so a re-poll harmlessly no-ops on posts
// already ingested.
func (s *Service) ingestAuthorFeed(ctx context.Context, did string) {
	out, err := bsky.FeedGetAuthorFeed(ctx, s.appviewClient(), did, "", "posts_no_replies", false, authorFeedFetchLimit)
	if err != nil {
		log.Printf("atproto: could not fetch author feed for %s: %v", did, err)
		return
	}

	for _, item := range out.Feed {
		if item.Post == nil || item.Reason != nil {
			continue // skip reposts surfaced in the author feed — not this account's own post
		}
		post := item.Post
		rec, ok := post.Record.Val.(*bsky.FeedPost)
		if !ok || rec == nil {
			continue
		}

		var handle, displayName, avatarURL string
		if post.Author != nil {
			handle = post.Author.Handle
			if post.Author.DisplayName != nil {
				displayName = *post.Author.DisplayName
			}
			if post.Author.Avatar != nil {
				avatarURL = *post.Author.Avatar
			}
		}
		// AGORA-205: checked from ingestion's first version, not bolted on
		// after the fact the way AGORA-148 found this gap on the AP side.
		if s.isBlueskyActorBlocked(did, handle) {
			continue
		}
		authorID, err := s.getOrCreateRemoteATUser(did, handle, displayName, avatarURL, "")
		if err != nil {
			log.Printf("atproto: could not upsert remote user for %s: %v", did, err)
			continue
		}

		var postID string
		err = s.db.QueryRowContext(ctx, `
			INSERT INTO posts (author_id, content, visibility, parent_id, is_remote, remote_post_id, remote_instance, remote_post_cid, content_warning)
			VALUES ($1, $2, 'public', NULL, true, $3, 'bsky.app', $4, $5)
			ON CONFLICT (remote_post_id, remote_instance) WHERE is_remote = true AND remote_post_id != '' DO NOTHING
			RETURNING id
		`, authorID, rec.Text, post.Uri, post.Cid, contentWarningFromLabels(rec.Labels)).Scan(&postID)
		if err != nil {
			continue // ErrNoRows on redelivery/already-ingested — expected, not an error
		}

		s.storeInboundEmbed(ctx, postID, post.Embed)
		s.storeHashtagsFromFacets(postID, rec.Facets) // AGORA-213

		// AGORA-198: notify local users who actively follow this DID, have
		// the global atproto_notifications_enabled toggle on, AND have
		// specifically opted into notifications for this account (af.notify)
		// — mirrors AGORA-160/166's ap_following loop. Runs only on the
		// actual first insert above (ON CONFLICT DO NOTHING made postID
		// empty and continued otherwise), so a redelivered/re-polled post
		// never fires a duplicate notification.
		if s.notif != nil {
			rows, err := s.db.QueryContext(ctx, `
				SELECT af.local_user_id
				FROM at_following af JOIN users u ON u.id = af.local_user_id
				WHERE af.remote_did = $1 AND af.notify = true AND u.atproto_notifications_enabled = true
			`, did)
			if err == nil {
				for rows.Next() {
					var followerID string
					if rows.Scan(&followerID) == nil {
						s.notif.Create(followerID, authorID, "atproto_post", postID, "")
					}
				}
				rows.Close()
			}
		}

		log.Printf("atproto: ingested post %s from %s (%s)", postID, handle, post.Uri)
	}
}

// StartBlueskyIngestion polls every followed DID's author feed on an
// interval (AGORA-197). Runs continuously rather than gating on any
// enabled flag at startup — same anti-pattern federation.StartBackgroundSync
// warns against — though unlike creates, polling reads are harmless to run
// regardless of the caller's own atproto_enabled state (this instance is
// only ever reading another account's already-public posts, not writing).
func (s *Service) StartBlueskyIngestion(ctx context.Context) {
	ticker := time.NewTicker(authorFeedPollInterval)
	defer ticker.Stop()

	poll := func() {
		rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT remote_did FROM at_following`)
		if err != nil {
			return
		}
		var dids []string
		for rows.Next() {
			var did string
			if rows.Scan(&did) == nil {
				dids = append(dids, did)
			}
		}
		rows.Close()

		for _, did := range dids {
			s.ingestAuthorFeed(ctx, did)
		}

		// AGORA-199: same ticker, same polling philosophy — check Bluesky
		// replies on Agora's own broadcast posts, scaling with how many posts
		// Agora federated rather than with the whole network's firehose.
		s.pollInboundReplies(ctx)

		// AGORA-200: same ticker again, for Bluesky likes/reposts on Agora's
		// own broadcast posts.
		s.pollInboundReactions(ctx)
	}

	poll()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
		}
	}
}
