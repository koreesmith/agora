package atproto

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/events"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/gorilla/websocket"
	"github.com/ipfs/go-cid"
)

var firehoseUpgrader = websocket.Upgrader{
	// The firehose is a public sync endpoint by design — any relay/AppView
	// needs to be able to open it, the same way federation's inbox and
	// actor/WebFinger endpoints are reachable by any fediverse server rather
	// than an allow-listed set.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// SubscribeRepos serves GET /xrpc/com.atproto.sync.subscribeRepos (AGORA-191)
// — the websocket firehose a relay subscribes to in order to discover and
// index this instance's repos. A `cursor` query param resumes from that
// sequence number (via pgEventPersister.Playback) instead of replaying
// everything or dropping events the subscriber missed while disconnected.
func (s *Service) SubscribeRepos(w http.ResponseWriter, r *http.Request) {
	if !s.atprotoEnabled() {
		writeError(w, 404, "AT Proto not enabled")
		return
	}
	conn, err := firehoseUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("atproto: firehose upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	var since *int64
	if c := r.URL.Query().Get("cursor"); c != "" {
		if n, err := strconv.ParseInt(c, 10, 64); err == nil {
			since = &n
		}
	}

	ctx := r.Context()
	evtChan, cleanup, err := s.events.Subscribe(ctx, r.RemoteAddr, nil, since)
	if err != nil {
		log.Printf("atproto: firehose subscribe failed: %v", err)
		return
	}
	defer cleanup()

	// AGORA-243: connect/disconnect were previously silent — the only
	// firehose log lines were upgrade/subscribe *failures* — so there was no
	// way to tell, after the fact, whether a relay had actually stayed
	// attached to the firehose or silently dropped off without erroring.
	// That ambiguity directly blocked diagnosing a real "is bsky.network
	// still subscribed at all" question: nginx's access log only records a
	// streaming connection when it *closes*, so zero log lines for hours
	// could mean either "still healthy" or "gave up and never came back."
	connectedAt := time.Now()
	sent := 0
	log.Printf("atproto: firehose subscriber connected from %s (cursor=%v)", r.RemoteAddr, since)
	defer func() {
		log.Printf("atproto: firehose subscriber %s disconnected after %s, %d event(s) sent",
			r.RemoteAddr, time.Since(connectedAt).Round(time.Second), sent)
	}()

	for {
		select {
		case evt, ok := <-evtChan:
			if !ok {
				return
			}
			wc, err := conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				return
			}
			if err := evt.Serialize(wc); err != nil {
				wc.Close()
				return
			}
			if err := wc.Close(); err != nil {
				return
			}
			sent++
		case <-ctx.Done():
			return
		}
	}
}

// emitCommit builds and pushes a #commit firehose event for a just-completed
// repo commit. Failure here is logged, not returned as a hard error to the
// caller (repo.go's commitAndPersist) — the repo write itself already
// succeeded and is durable; missing a firehose emission means subscribers
// miss one event, not that the commit is lost or invalid, so it shouldn't
// fail the write that triggered it.
func (s *Service) emitCommit(ctx context.Context, did string, commitCid cid.Cid, rev, sinceRev string, blocks map[string][]byte, ops []*comatproto.SyncSubscribeRepos_RepoOp) {
	var carBuf bytes.Buffer
	if err := writeCommitCAR(&carBuf, commitCid, blocks); err != nil {
		log.Printf("atproto: could not encode commit CAR for %s: %v", commitCid, err)
		return
	}

	var since *string
	if sinceRev != "" {
		since = &sinceRev
	}

	evt := &events.XRPCStreamEvent{
		RepoCommit: &comatproto.SyncSubscribeRepos_Commit{
			Repo:   did,
			Commit: lexutil.LexLink(commitCid),
			Rev:    rev,
			Since:  since,
			Time:   time.Now().UTC().Format(time.RFC3339),
			Blocks: carBuf.Bytes(),
			Ops:    ops,
			Blobs:  []lexutil.LexLink{},
		},
	}
	if err := s.events.AddEvent(ctx, evt); err != nil {
		log.Printf("atproto: could not emit firehose event for commit %s: %v", commitCid, err)
	}
}
