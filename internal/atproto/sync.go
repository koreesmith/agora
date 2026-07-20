package atproto

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"strings"

	arepo "github.com/bluesky-social/indigo/repo"
	"github.com/ipfs/go-cid"
)

// openRepoForRead opens a user's repo read-only off their persisted commit
// head — the getRecord/listRecords counterpart to getOrCreateRepo (repo.go),
// which is write-oriented (it lazily creates a repo if none exists yet). A
// read against a user with no repo, or an unreadable head, has nothing
// meaningful to return either way, so both cases collapse to "not found"
// rather than materializing an empty repo just to read from it.
func (s *Service) openRepoForRead(ctx context.Context, userID, repoHead string) (*arepo.Repo, bool) {
	if repoHead == "" {
		return nil, false
	}
	root, err := cid.Decode(repoHead)
	if err != nil {
		return nil, false
	}
	bs := &pgBlockstore{db: s.db, userID: userID}
	r, err := arepo.OpenRepo(ctx, bs, root)
	if err != nil {
		return nil, false
	}
	return r, true
}

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

// listReposDefaultLimit/listReposMaxLimit mirror the reference PDS's own
// com.atproto.sync.listRepos bounds.
const (
	listReposDefaultLimit = 500
	listReposMaxLimit     = 1000
)

// ListRepos serves GET /xrpc/com.atproto.sync.listRepos (AGORA-232) — the
// account-discovery endpoint a relay calls after accepting a crawl request
// to learn which DIDs actually live on this (multi-tenant) host at all.
// Without this, GetRepo/GetLatestCommit/GetBlocks (AGORA-231) have nothing
// to be called with in the first place: a relay can only learn about a DID
// reactively, from a live firehose commit naming it, never proactively.
// Cursor-paginated on user id, the same opaque-string-cursor shape
// getAuthorFeed etc. already use elsewhere in Agora's own API.
func (s *Service) ListRepos(w http.ResponseWriter, r *http.Request) {
	if !s.atprotoEnabled() {
		writeError(w, 404, "AT Proto not enabled")
		return
	}

	limit := listReposDefaultLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= listReposMaxLimit {
			limit = n
		}
	}
	cursor := r.URL.Query().Get("cursor")

	// AGORA-240: id is uuid — SQL doesn't short-circuit OR, so Postgres
	// still type-checks "id > $1" even when the empty-cursor branch is the
	// one that's true, and casting "" to uuid fails outright ("invalid
	// input syntax for type uuid"). This 500'd on literally every
	// no-cursor (first-page) call, which is the relay's normal case.
	// Branching in Go instead of relying on SQL to skip the comparison
	// avoids ever handing an empty string to a uuid comparison at all.
	const baseQuery = `
		SELECT id, atproto_did, atproto_repo_head, atproto_repo_rev
		FROM users
		WHERE is_remote = false AND profile_private = false AND atproto_enabled = true
		  AND deletion_scheduled_at IS NULL AND atproto_repo_head != ''
	`
	var rows *sql.Rows
	var err error
	if cursor == "" {
		rows, err = s.db.QueryContext(r.Context(), baseQuery+` ORDER BY id ASC LIMIT $1`, limit)
	} else {
		rows, err = s.db.QueryContext(r.Context(), baseQuery+` AND id > $1 ORDER BY id ASC LIMIT $2`, cursor, limit)
	}
	if err != nil {
		writeError(w, 500, "could not list repos")
		return
	}
	defer rows.Close()

	// repos must marshal as [] rather than null when empty — the lexicon's
	// "repos" field has no omitempty, and a null there is not a valid empty
	// array to a strict client.
	repos := []map[string]any{}
	var lastID string
	for rows.Next() {
		var id, did, head, rev string
		if err := rows.Scan(&id, &did, &head, &rev); err != nil {
			writeError(w, 500, "could not list repos")
			return
		}
		repos = append(repos, map[string]any{"did": did, "head": head, "rev": rev})
		lastID = id
	}

	out := map[string]any{"repos": repos}
	if len(repos) == limit {
		out["cursor"] = lastID
	}
	writeJSON(w, 200, out)
}

// GetBlob serves GET /xrpc/com.atproto.sync.getBlob (AGORA-235) — fetches a
// single blob's raw bytes by (did, cid), e.g. for Bluesky's image CDN to
// fetch and cache an avatar/banner (AGORA-233) or post image (AGORA-194)
// the first time it's requested. Without this, a blob CID referenced from
// a record points at bytes no consumer can actually retrieve, even once
// the record itself indexes correctly. Content-Type is sniffed rather than
// stored, mirroring readLocalImage's same approach for local files —
// pgBlockstore only ever holds raw bytes, no separate mimetype column.
func (s *Service) GetBlob(w http.ResponseWriter, r *http.Request) {
	u, ok := s.eligibleUserByDID(r.URL.Query().Get("did"))
	if !ok {
		writeError(w, 404, "not found")
		return
	}
	c, err := cid.Decode(r.URL.Query().Get("cid"))
	if err != nil {
		writeError(w, 400, "invalid cid")
		return
	}

	bs := &pgBlockstore{db: s.db, userID: u.ID}
	blk, err := bs.Get(r.Context(), c)
	if err != nil {
		writeError(w, 404, "blob not found")
		return
	}

	data := blk.RawData()
	w.Header().Set("Content-Type", http.DetectContentType(data))
	w.WriteHeader(200)
	w.Write(data)
}

// ListRecords serves GET /xrpc/com.atproto.repo.listRecords (AGORA-241).
// This was the missing piece behind profile pictures (and, per the theory
// that motivated adding this, plausibly some posts too) never indexing on
// Bluesky: posts/likes/follows/reposts each get one commit and one firehose
// event for their entire lifetime, so a relay that caught the live commit
// has everything it'll ever need. app.bsky.actor.profile is different — it's
// a mutable singleton record at a fixed rkey ("self"), rewritten in place on
// every profile edit, and an AppView calls listRecords directly against the
// PDS to re-fetch its current value rather than trusting it stays in sync
// from commits alone. Every call 404'd (this endpoint didn't exist at all),
// so the AppView never had a way to actually read the current profile.
//
// Walks the MST directly off the repo's persisted head rather than
// exporting/replaying the full CAR (GetRepo) — this only ever needs one
// collection's worth of records, typically a handful.
func (s *Service) ListRecords(w http.ResponseWriter, r *http.Request) {
	u, ok := s.eligibleUserByDID(r.URL.Query().Get("repo"))
	if !ok {
		writeError(w, 404, "repo not found")
		return
	}
	collection := r.URL.Query().Get("collection")
	if collection == "" {
		writeError(w, 400, "missing collection")
		return
	}

	limit := listReposDefaultLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= listReposMaxLimit {
			limit = n
		}
	}
	cursor := r.URL.Query().Get("cursor")
	reverse := r.URL.Query().Get("reverse") == "true"

	var repoHead string
	if err := s.db.QueryRow(`SELECT atproto_repo_head FROM users WHERE id = $1`, u.ID).Scan(&repoHead); err != nil {
		writeJSON(w, 200, map[string]any{"records": []map[string]any{}})
		return
	}
	repo, ok := s.openRepoForRead(r.Context(), u.ID, repoHead)
	if !ok {
		writeJSON(w, 200, map[string]any{"records": []map[string]any{}})
		return
	}

	prefix := collection + "/"
	var rkeys []string
	repo.ForEach(r.Context(), prefix, func(k string, _ cid.Cid) error {
		if !strings.HasPrefix(k, prefix) {
			return arepo.ErrDoneIterating
		}
		rkeys = append(rkeys, strings.TrimPrefix(k, prefix))
		return nil
	})
	if reverse {
		for i, j := 0, len(rkeys)-1; i < j; i, j = i+1, j-1 {
			rkeys[i], rkeys[j] = rkeys[j], rkeys[i]
		}
	}

	records := []map[string]any{}
	for _, rkey := range rkeys {
		if cursor != "" {
			if (!reverse && rkey <= cursor) || (reverse && rkey >= cursor) {
				continue
			}
		}
		if len(records) >= limit {
			break
		}
		rc, val, err := repo.GetRecord(r.Context(), prefix+rkey)
		if err != nil {
			continue
		}
		records = append(records, map[string]any{
			"uri":   "at://" + u.AtprotoDID + "/" + prefix + rkey,
			"cid":   rc.String(),
			"value": val,
		})
	}

	out := map[string]any{"records": records}
	if len(records) == limit && len(records) > 0 {
		out["cursor"] = rkeys[len(rkeys)-1]
	}
	writeJSON(w, 200, out)
}

// GetRecord serves GET /xrpc/com.atproto.repo.getRecord — getRecord's
// single-record counterpart to ListRecords, e.g. for a client resolving one
// specific at:// URI (a quote-post's target, a like's subject) directly
// against the origin PDS rather than through an AppView.
func (s *Service) GetRecord(w http.ResponseWriter, r *http.Request) {
	u, ok := s.eligibleUserByDID(r.URL.Query().Get("repo"))
	if !ok {
		writeError(w, 404, "repo not found")
		return
	}
	collection := r.URL.Query().Get("collection")
	rkey := r.URL.Query().Get("rkey")
	if collection == "" || rkey == "" {
		writeError(w, 400, "missing collection or rkey")
		return
	}

	var repoHead string
	if err := s.db.QueryRow(`SELECT atproto_repo_head FROM users WHERE id = $1`, u.ID).Scan(&repoHead); err != nil {
		writeError(w, 404, "record not found")
		return
	}
	repo, ok := s.openRepoForRead(r.Context(), u.ID, repoHead)
	if !ok {
		writeError(w, 404, "record not found")
		return
	}

	rpath := collection + "/" + rkey
	rc, val, err := repo.GetRecord(r.Context(), rpath)
	if err != nil {
		writeError(w, 404, "record not found")
		return
	}

	writeJSON(w, 200, map[string]any{
		"uri":   "at://" + u.AtprotoDID + "/" + rpath,
		"cid":   rc.String(),
		"value": val,
	})
}
