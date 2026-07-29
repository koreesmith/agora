package atproto

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/go-chi/chi/v5"
)

// blueskyBackfillBatchSize is app.bsky.feed.getPosts' own documented cap on
// how many AT-URIs a single call accepts.
const blueskyBackfillBatchSize = 25

// RegisterAdminRoutes wires the one-time published_at backfill (AGORA-270
// follow-up), gated by RequireAdmin at the router-group level in
// cmd/server/main.go — the same pattern federation.RegisterAdminRoutes uses
// for a non-admin package's own admin-only route. Lives here rather than in
// admin.go because re-fetching a post's real origin timestamp needs the
// AppView client that only exists in this package.
func RegisterAdminRoutes(r chi.Router, s *Service) {
	r.Post("/admin/atproto/backfill-published-at", s.BackfillPublishedAt)
}

// BackfillPublishedAt re-fetches the real record.createdAt for every
// already-ingested Bluesky post and corrects its published_at, which the
// AGORA-270 migration could only backfill to the existing (ingestion-time)
// created_at for rows that predate that fix — the real origin timestamp was
// never captured before then, so there was nothing else to backfill it from
// at migration time. Runs in the background and returns immediately; poll
// the server log for progress/completion, matching how this codebase's
// other long-running admin-triggered operations (e.g. relay crawl) report
// status.
func (s *Service) BackfillPublishedAt(w http.ResponseWriter, r *http.Request) {
	go s.runPublishedAtBackfill(context.Background())
	writeJSON(w, 202, map[string]string{"status": "backfill started, check server logs for progress"})
}

func (s *Service) runPublishedAtBackfill(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, remote_post_id FROM posts
		WHERE is_remote = true AND remote_instance = 'bsky.app' AND remote_post_id != '' AND deleted_at IS NULL
	`)
	if err != nil {
		log.Printf("atproto: published_at backfill: could not list posts: %v", err)
		return
	}
	type row struct{ id, uri string }
	var all []row
	for rows.Next() {
		var rr row
		if rows.Scan(&rr.id, &rr.uri) == nil {
			all = append(all, rr)
		}
	}
	rows.Close()

	log.Printf("atproto: published_at backfill: starting on %d Bluesky posts", len(all))
	client := s.appviewClient()
	var updated, failed int
	for i := 0; i < len(all); i += blueskyBackfillBatchSize {
		end := i + blueskyBackfillBatchSize
		if end > len(all) {
			end = len(all)
		}
		batch := all[i:end]
		uriToID := make(map[string]string, len(batch))
		uris := make([]string, 0, len(batch))
		for _, rr := range batch {
			uriToID[rr.uri] = rr.id
			uris = append(uris, rr.uri)
		}

		out, err := bsky.FeedGetPosts(ctx, client, uris)
		if err != nil {
			// A deleted/blocked/unreachable post in the batch fails the whole
			// call (getPosts has no partial-failure shape) — the remaining
			// batches are independent and still worth attempting.
			log.Printf("atproto: published_at backfill: batch fetch failed (%d posts skipped): %v", len(batch), err)
			failed += len(batch)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		for _, post := range out.Posts {
			id, ok := uriToID[post.Uri]
			if !ok {
				continue
			}
			rec, ok := post.Record.Val.(*bsky.FeedPost)
			if !ok || rec == nil {
				failed++
				continue
			}
			if _, err := s.db.ExecContext(ctx, `UPDATE posts SET published_at = $1 WHERE id = $2`,
				parseBlueskyTime(rec.CreatedAt), id); err != nil {
				failed++
				continue
			}
			updated++
		}
		// A batch missing some URIs entirely (deleted/blocked accounts) just
		// leaves those rows' published_at unchanged rather than erroring.
		time.Sleep(500 * time.Millisecond)
	}
	log.Printf("atproto: published_at backfill: done — %d updated, %d failed/skipped, %d total", updated, failed, len(all))
}
