package atproto

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/agora-social/agora/internal/media"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

// maxImageBlobBytes is Bluesky's cap on an image blob (app.bsky.embed.images
// declares maxSize 1000000). Larger blobs are accepted by the record but the
// AppView refuses to serve them, so the image renders as a black box.
const maxImageBlobBytes = 1000000

// blobCID computes the content-addressed CID a blob is referenced by —
// CIDv1, raw codec (not dag-cbor, since a blob isn't an MST/record node —
// repomgr's own walkTree explicitly skips "raw" CIDs it finds inside a
// record for this exact reason), sha256 multihash, matching how AT Proto
// blobs are identified everywhere else in the ecosystem.
func blobCID(data []byte) (cid.Cid, error) {
	mh, err := multihash.Sum(data, multihash.SHA2_256, -1)
	if err != nil {
		return cid.Undef, err
	}
	return cid.NewCidV1(cid.Raw, mh), nil
}

// readLocalUpload reads an already-uploaded post image or video straight off
// local disk rather than fetching it over HTTP the way postImages's
// federation counterpart does for remote fediverse consumers. This instance
// already has this exact file on its own filesystem (internal/media's
// UploadDir), so a self-fetch would just be slower for no benefit.
func readLocalUpload(uploadDir, url string) (data []byte, mimeType string, err error) {
	rel := strings.TrimPrefix(url, "/uploads/")
	if rel == url {
		return nil, "", &os.PathError{Op: "read", Path: url, Err: os.ErrNotExist}
	}
	data, err = os.ReadFile(filepath.Join(uploadDir, rel))
	if err != nil {
		return nil, "", err
	}
	return data, http.DetectContentType(data), nil
}

// postImage is one image attached to a post, with its description
// (AGORA-400; "" when it has none).
type postImage struct {
	URL string
	Alt string
}

// postImages is this package's own copy of federation's postImages,
// duplicated rather than imported, the same tradeoff already made for
// domainFromURL (a small query isn't worth a cross-package dependency).
// Unlike federation's version, this deliberately returns storage-relative
// URLs (not absolute ones) since the caller reads the file locally rather
// than handing the URL to a remote server.
func (s *Service) postImages(ctx context.Context, postID string) []postImage {
	rows, err := s.db.QueryContext(ctx, `SELECT url, alt FROM post_photos WHERE post_id = $1 ORDER BY position ASC`, postID)
	if err == nil {
		defer rows.Close()
		var images []postImage
		for rows.Next() {
			var u, alt string
			if rows.Scan(&u, &alt) == nil && u != "" {
				images = append(images, postImage{URL: u, Alt: alt})
			}
		}
		if len(images) > 0 {
			return images
		}
	}
	var imageURL, imageAlt string
	s.db.QueryRowContext(ctx, `SELECT image_url, image_alt FROM posts WHERE id = $1`, postID).Scan(&imageURL, &imageAlt)
	if imageURL != "" {
		return []postImage{{URL: imageURL, Alt: imageAlt}}
	}
	return nil
}

// uploadBlob reads an already-uploaded local file (image or video) and
// stores it as a content-addressed blob in bs, returning the LexBlob ref a
// record embeds it with. Shared by buildImageEmbed (post images, AGORA-194),
// buildVideoEmbed (post video, AGORA-368), and SyncProfile (avatar/banner,
// AGORA-233), since the upload step itself doesn't care what kind of file
// it is.
func (s *Service) uploadBlob(ctx context.Context, bs *pgBlockstore, url string) (*lexutil.LexBlob, error) {
	data, mimeType, err := readLocalUpload(s.cfg.UploadDir, url)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(mimeType, "image/") && len(data) > maxImageBlobBytes {
		shrunk, mt, err := media.ShrinkToFit(data, maxImageBlobBytes)
		if err != nil {
			return nil, err
		}
		data, mimeType = shrunk, mt
	}
	c, err := blobCID(data)
	if err != nil {
		return nil, err
	}
	blk, err := blocks.NewBlockWithCid(data, c)
	if err != nil {
		return nil, err
	}
	if err := bs.Put(ctx, blk); err != nil {
		return nil, err
	}
	return &lexutil.LexBlob{
		Ref:      lexutil.LexLink(c),
		MimeType: mimeType,
		Size:     int64(len(data)),
	}, nil
}

// buildImageEmbed uploads each of a post's images as a content-addressed
// blob into this user's own block store (AGORA-194): pgBlockstore's flat
// (user_id, cid, data) shape holds a raw blob exactly as easily as an
// MST/commit node, so no new storage plumbing is needed beyond uploadBlob.
// Truncates to AT Proto's 4-image embed cap rather than erroring the whole
// post federation attempt if Agora's own post_photos limit (currently
// higher) was used. Each image carries its description as alt (AGORA-400),
// "" when the author didn't write one.
func (s *Service) buildImageEmbed(ctx context.Context, bs *pgBlockstore, postID string) *bsky.FeedPost_Embed {
	imgs := s.postImages(ctx, postID)
	if len(imgs) == 0 {
		return nil
	}
	if len(imgs) > 4 {
		imgs = imgs[:4]
	}

	var images []*bsky.EmbedImages_Image
	for _, img := range imgs {
		blob, err := s.uploadBlob(ctx, bs, img.URL)
		if err != nil {
			log.Printf("atproto: could not upload image blob for %s: %v", img.URL, err)
			continue
		}
		images = append(images, &bsky.EmbedImages_Image{Image: blob, Alt: img.Alt})
	}
	if len(images) == 0 {
		return nil
	}
	return &bsky.FeedPost_Embed{EmbedImages: &bsky.EmbedImages{
		LexiconTypeID: "app.bsky.embed.images",
		Images:        images,
	}}
}

// postVideoURL returns a post's attached video's storage-relative URL
// (AGORA-368), mirroring postImages. A post has at most one video
// (AGORA-137's own composer treats video and photos as mutually exclusive),
// so unlike post_photos there's no ordered multi-row table behind it.
func (s *Service) postVideoURL(ctx context.Context, postID string) string {
	var videoURL string
	s.db.QueryRowContext(ctx, `SELECT video_url FROM posts WHERE id = $1`, postID).Scan(&videoURL)
	return videoURL
}

// buildVideoEmbed is buildImageEmbed's video counterpart. AT Proto's video
// embed (app.bsky.embed.video) takes a single blob the same way an image
// does; Agora's own transcode pipeline (AGORA-137, 720p H.264) already keeps
// the output well under Bluesky's 100MB cap in every realistic case, so no
// separate size check is added here, a failed upload (oversized or
// otherwise) is simply logged and dropped rather than blocking the post.
func (s *Service) buildVideoEmbed(ctx context.Context, bs *pgBlockstore, postID string) *bsky.FeedPost_Embed {
	url := s.postVideoURL(ctx, postID)
	if url == "" {
		return nil
	}
	blob, err := s.uploadBlob(ctx, bs, url)
	if err != nil {
		log.Printf("atproto: could not upload video blob for %s: %v", url, err)
		return nil
	}
	return &bsky.FeedPost_Embed{EmbedVideo: &bsky.EmbedVideo{
		LexiconTypeID: "app.bsky.embed.video",
		Video:         blob,
	}}
}

// buildMediaEmbed picks whichever of image or video a post actually has
// (AGORA-368) since FeedPost.Embed only ever holds one, and the compose UI
// already keeps the two mutually exclusive, so at most one of these two ever
// returns anything.
func (s *Service) buildMediaEmbed(ctx context.Context, bs *pgBlockstore, postID string) *bsky.FeedPost_Embed {
	if embed := s.buildImageEmbed(ctx, bs, postID); embed != nil {
		return embed
	}
	return s.buildVideoEmbed(ctx, bs, postID)
}
