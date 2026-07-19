package atproto

import (
	"log"
	"net/http"

	"github.com/ipfs/go-cid"
)

// GetRepo serves GET /xrpc/com.atproto.sync.getRepo (AGORA-231) — the
// backfill counterpart to SubscribeRepos' live tail. A relay calls this once
// per newly-discovered repo to pull everything that existed before it
// subscribed; without it, a relay that accepts our crawl request (AGORA-230)
// still has no way to learn about any pre-existing post/reply/like/repost,
// only ones committed from that point forward. "since" (incremental sync
// from a prior rev) isn't implemented — every call returns the full repo,
// which is always a valid (if less efficient) answer to a diff request.
func (s *Service) GetRepo(w http.ResponseWriter, r *http.Request) {
	u, ok := s.eligibleUserByDID(r.URL.Query().Get("did"))
	if !ok {
		writeError(w, 404, "repo not found")
		return
	}

	var repoHead string
	if err := s.db.QueryRow(`SELECT atproto_repo_head FROM users WHERE id = $1`, u.ID).Scan(&repoHead); err != nil || repoHead == "" {
		writeError(w, 404, "repo not found")
		return
	}
	root, err := cid.Decode(repoHead)
	if err != nil {
		writeError(w, 500, "corrupt repo head")
		return
	}

	bs := &pgBlockstore{db: s.db, userID: u.ID}
	blocks, err := bs.AllBlocks(r.Context())
	if err != nil {
		writeError(w, 500, "could not read repo")
		return
	}

	w.Header().Set("Content-Type", "application/vnd.ipld.car")
	w.WriteHeader(200)
	if err := writeCAR(w, []cid.Cid{root}, blocks); err != nil {
		log.Printf("atproto: could not encode repo CAR for user %s: %v", u.ID, err)
	}
}

// GetLatestCommit serves GET /xrpc/com.atproto.sync.getLatestCommit — the
// cheap head/rev check a relay polls to decide whether it needs to re-sync
// a repo at all, without pulling the full CAR every time.
func (s *Service) GetLatestCommit(w http.ResponseWriter, r *http.Request) {
	u, ok := s.eligibleUserByDID(r.URL.Query().Get("did"))
	if !ok {
		writeError(w, 404, "repo not found")
		return
	}

	var repoHead, repoRev string
	if err := s.db.QueryRow(`SELECT atproto_repo_head, atproto_repo_rev FROM users WHERE id = $1`, u.ID).
		Scan(&repoHead, &repoRev); err != nil || repoHead == "" {
		writeError(w, 404, "repo not found")
		return
	}
	writeJSON(w, 200, map[string]any{"cid": repoHead, "rev": repoRev})
}

// GetBlocks serves GET /xrpc/com.atproto.sync.getBlocks — fetches specific
// blocks by CID out of a repo, e.g. for a relay resolving a reference it
// hit while walking a commit it already has rather than re-fetching the
// whole repo. Unknown/undecodable CIDs are silently skipped rather than
// failing the whole request, matching a relay only ever asking for CIDs it
// has some independent reason to believe exist. The resolved CIDs double as
// the CAR's roots list: CARv1 readers (go-car among them) reject a car with
// zero roots outright, and there's no other meaningful root for an
// arbitrary bag of blocks.
func (s *Service) GetBlocks(w http.ResponseWriter, r *http.Request) {
	u, ok := s.eligibleUserByDID(r.URL.Query().Get("did"))
	if !ok {
		writeError(w, 404, "repo not found")
		return
	}

	bs := &pgBlockstore{db: s.db, userID: u.ID}
	blocks := make(map[string][]byte)
	var roots []cid.Cid
	for _, cidStr := range r.URL.Query()["cids"] {
		c, err := cid.Decode(cidStr)
		if err != nil {
			continue
		}
		if blk, err := bs.Get(r.Context(), c); err == nil {
			blocks[cidStr] = blk.RawData()
			roots = append(roots, c)
		}
	}
	if len(roots) == 0 {
		// A zero-root CAR is invalid per CARv1 (go-car's reader rejects it
		// outright) — better to fail the request than emit bytes no client
		// can parse.
		writeError(w, 404, "no requested blocks found")
		return
	}

	w.Header().Set("Content-Type", "application/vnd.ipld.car")
	w.WriteHeader(200)
	if err := writeCAR(w, roots, blocks); err != nil {
		log.Printf("atproto: could not encode blocks CAR for user %s: %v", u.ID, err)
	}
}
