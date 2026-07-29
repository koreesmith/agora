package federation

import (
	"context"
	"log"
	"net/http"
	"time"
)

// fedBackfillDelay throttles re-fetches across potentially hundreds of
// distinct remote domains — a plain per-request pause rather than
// per-instance batching, since a single backfill run is a one-time
// maintenance operation, not something latency-sensitive.
const fedBackfillDelay = 300 * time.Millisecond

// BackfillPublishedAt re-fetches the real "published" timestamp for every
// already-ingested Fediverse post and corrects its published_at — the
// AGORA-270 migration could only backfill to the existing (ingestion-time)
// created_at for rows that predate that fix, since the real origin
// timestamp was never captured before then. Runs in the background and
// returns immediately; poll the server log for progress/completion.
func (s *Service) BackfillPublishedAt(w http.ResponseWriter, r *http.Request) {
	go s.runPublishedAtBackfill(context.Background())
	writeJSON(w, 202, map[string]string{"status": "backfill started, check server logs for progress"})
}

func (s *Service) runPublishedAtBackfill(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, remote_post_id FROM posts
		WHERE is_remote = true AND remote_instance != '' AND remote_instance != 'bsky.app'
		  AND remote_post_id != '' AND deleted_at IS NULL
	`)
	if err != nil {
		log.Printf("federation: published_at backfill: could not list posts: %v", err)
		return
	}
	type row struct{ id, url string }
	var all []row
	for rows.Next() {
		var rr row
		if rows.Scan(&rr.id, &rr.url) == nil {
			all = append(all, rr)
		}
	}
	rows.Close()

	log.Printf("federation: published_at backfill: starting on %d Fediverse posts", len(all))
	var updated, failed int
	for _, rr := range all {
		note, err := s.fetchRemoteNoteSignedAsInstance(rr.url)
		if err != nil || note == nil || note.Published == "" {
			// Deleted, unreachable, or an instance that just doesn't set
			// "published" (rare, but not worth failing the run over) —
			// leave the existing (ingestion-time) value alone.
			failed++
			time.Sleep(fedBackfillDelay)
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE posts SET published_at = $1 WHERE id = $2`,
			parseAPTime(note.Published), rr.id); err != nil {
			failed++
			time.Sleep(fedBackfillDelay)
			continue
		}
		updated++
		time.Sleep(fedBackfillDelay)
	}
	log.Printf("federation: published_at backfill: done — %d updated, %d failed/skipped, %d total", updated, failed, len(all))
}
