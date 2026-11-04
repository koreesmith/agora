package atproto

import (
	"context"
	"errors"

	"github.com/bluesky-social/indigo/api/bsky"
)

// IngestPostByURI makes sure the Bluesky post at atURI is stored here as an
// ordinary remote post and returns its local id. AGORA-453: a quote-share from
// another Agora instance names the quoted Bluesky post only by its web URL, so
// this instance may never have seen it. Fetched from the AppView the same way
// every other inbound Bluesky post is, and stored by ingestQuotedPost.
func (s *Service) IngestPostByURI(ctx context.Context, atURI string) (string, error) {
	if !s.atprotoEnabled() {
		return "", errors.New("AT Proto not enabled")
	}
	var id string
	s.db.QueryRowContext(ctx, `SELECT id FROM posts WHERE remote_post_id = $1 AND remote_instance = 'bsky.app' AND deleted_at IS NULL LIMIT 1`, atURI).Scan(&id)
	if id != "" {
		return id, nil
	}

	out, err := bsky.FeedGetPosts(ctx, s.appviewClient(), []string{atURI})
	if err != nil {
		return "", err
	}
	if len(out.Posts) == 0 || out.Posts[0] == nil {
		return "", errors.New("post not found")
	}
	pv := out.Posts[0]
	rec := &bsky.EmbedRecord_ViewRecord{
		Author: pv.Author,
		Cid:    pv.Cid,
		Uri:    pv.Uri,
		Labels: pv.Labels,
		Value:  pv.Record,
	}
	if e := pv.Embed; e != nil {
		rec.Embeds = []*bsky.EmbedRecord_ViewRecord_Embeds_Elem{{
			EmbedImages_View:          e.EmbedImages_View,
			EmbedVideo_View:           e.EmbedVideo_View,
			EmbedGallery_View:         e.EmbedGallery_View,
			EmbedExternal_View:        e.EmbedExternal_View,
			EmbedRecord_View:          e.EmbedRecord_View,
			EmbedRecordWithMedia_View: e.EmbedRecordWithMedia_View,
		}}
	}
	id = s.ingestQuotedPost(ctx, rec)
	if id == "" {
		return "", errors.New("could not store the post")
	}
	return id, nil
}
