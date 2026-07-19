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
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

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

// readLocalImage reads an already-uploaded post image straight off local
// disk rather than fetching it over HTTP the way postImageURLs's federation
// counterpart does for remote fediverse consumers — this instance already
// has this exact file on its own filesystem (internal/media's UploadDir),
// so a self-fetch would just be slower for no benefit.
func readLocalImage(uploadDir, url string) (data []byte, mimeType string, err error) {
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

// postImageURLs is this package's own copy of federation's postImageURLs —
// duplicated rather than imported, the same tradeoff already made for
// domainFromURL (a small query isn't worth a cross-package dependency).
// Unlike federation's version, this deliberately returns storage-relative
// URLs (not absolute ones) since the caller reads the file locally rather
// than handing the URL to a remote server.
func (s *Service) postImageURLs(ctx context.Context, postID string) []string {
	rows, err := s.db.QueryContext(ctx, `SELECT url FROM post_photos WHERE post_id = $1 ORDER BY position ASC`, postID)
	if err == nil {
		defer rows.Close()
		var urls []string
		for rows.Next() {
			var u string
			if rows.Scan(&u) == nil && u != "" {
				urls = append(urls, u)
			}
		}
		if len(urls) > 0 {
			return urls
		}
	}
	var imageURL string
	s.db.QueryRowContext(ctx, `SELECT image_url FROM posts WHERE id = $1`, postID).Scan(&imageURL)
	if imageURL != "" {
		return []string{imageURL}
	}
	return nil
}

// uploadImageBlob reads an already-uploaded local image and stores it as a
// content-addressed blob in bs, returning the LexBlob ref a record embeds it
// with. Shared by buildImageEmbed (post images, AGORA-194) and SyncProfile
// (avatar/banner, AGORA-233) — the upload step itself doesn't care what kind
// of image it is.
func (s *Service) uploadImageBlob(ctx context.Context, bs *pgBlockstore, url string) (*lexutil.LexBlob, error) {
	data, mimeType, err := readLocalImage(s.cfg.UploadDir, url)
	if err != nil {
		return nil, err
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
// blob into this user's own block store (AGORA-194) — pgBlockstore's flat
// (user_id, cid, data) shape holds a raw blob exactly as easily as an
// MST/commit node, so no new storage plumbing is needed beyond
// uploadImageBlob. Truncates to AT Proto's 4-image embed cap rather than
// erroring the whole post federation attempt if Agora's own post_photos
// limit (currently higher) was used. Alt text is left blank — Agora doesn't
// capture per-image alt text today; a real follow-up if that ever changes,
// not something to block this on.
func (s *Service) buildImageEmbed(ctx context.Context, bs *pgBlockstore, postID string) *bsky.FeedPost_Embed {
	urls := s.postImageURLs(ctx, postID)
	if len(urls) == 0 {
		return nil
	}
	if len(urls) > 4 {
		urls = urls[:4]
	}

	var images []*bsky.EmbedImages_Image
	for _, url := range urls {
		blob, err := s.uploadImageBlob(ctx, bs, url)
		if err != nil {
			log.Printf("atproto: could not upload image blob for %s: %v", url, err)
			continue
		}
		images = append(images, &bsky.EmbedImages_Image{Image: blob})
	}
	if len(images) == 0 {
		return nil
	}
	return &bsky.FeedPost_Embed{EmbedImages: &bsky.EmbedImages{
		LexiconTypeID: "app.bsky.embed.images",
		Images:        images,
	}}
}
