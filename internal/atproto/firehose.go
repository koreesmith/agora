package atproto

import (
	"bytes"
	"fmt"
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
	cursorStr := "none"
	if since != nil {
		cursorStr = strconv.FormatInt(*since, 10)
	}
	log.Printf("atproto: firehose subscriber connected from %s (cursor=%s)", r.RemoteAddr, cursorStr)
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

// buildCommitEvent assembles the #commit firehose event for a just-completed
// repo commit. It deliberately does not persist or broadcast: commitAndPersist
// persists it inside the same transaction that advances the repo head, and
// broadcasts only after that transaction commits, so the stored head and the
// firehose log can never drift out of sync (a head ahead of the firehose
// silently breaks every later commit's `since`; a firehose ahead of the head
// forks the chain on the next commit).
func (s *Service) buildCommitEvent(did string, commitCid cid.Cid, rev, sinceRev string, blocks map[string][]byte, ops []*comatproto.SyncSubscribeRepos_RepoOp) (*events.XRPCStreamEvent, error) {
	var carBuf bytes.Buffer
	if err := writeCommitCAR(&carBuf, commitCid, blocks); err != nil {
		return nil, fmt.Errorf("encoding commit CAR for %s: %w", commitCid, err)
	}

	var since *string
	if sinceRev != "" {
		since = &sinceRev
	}

	return &events.XRPCStreamEvent{
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
	}, nil
}
