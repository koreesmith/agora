package atproto

import (
	"context"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"

	"github.com/agora-social/agora/internal/store"
)

// pgBlockstore is a per-user, content-addressed block store backing an AT
// Proto repo's MST — the Postgres equivalent of the local-disk CAR/blockstore
// a reference PDS would use, kept consistent with the rest of Agora storing
// everything in Postgres rather than introducing a second storage system.
// Satisfies indigo's cbor.IpldBlockstore (Get/Put by CID), which is all the
// repo package needs.
//
// Also doubles as the source for a commit's firehose CAR slice (AGORA-191):
// while recording is on, every Put is captured in memory as well as written
// to Postgres, so the exact set of blocks a single commit touched (new/
// changed MST nodes, the record, the signed commit object itself) is known
// without having to diff the whole tree — a commit only ever Puts blocks
// that are new to it, so "what got Put during this commit" and "what's new
// since the previous commit" are the same set.
type pgBlockstore struct {
	db     *store.DB
	userID string

	recording bool
	recorded  map[string][]byte // cid string -> raw block data, insertion order not required
}

func (bs *pgBlockstore) startRecording() {
	bs.recording = true
	bs.recorded = make(map[string][]byte)
}

func (bs *pgBlockstore) stopRecording() map[string][]byte {
	bs.recording = false
	r := bs.recorded
	bs.recorded = nil
	return r
}

func (bs *pgBlockstore) Get(ctx context.Context, c cid.Cid) (blocks.Block, error) {
	var data []byte
	err := bs.db.QueryRowContext(ctx, `
		SELECT data FROM atproto_blocks WHERE user_id = $1 AND cid = $2
	`, bs.userID, c.String()).Scan(&data)
	if err != nil {
		return nil, err
	}
	return blocks.NewBlockWithCid(data, c)
}

func (bs *pgBlockstore) Put(ctx context.Context, b blocks.Block) error {
	if bs.recording {
		bs.recorded[b.Cid().String()] = b.RawData()
	}
	_, err := bs.db.ExecContext(ctx, `
		INSERT INTO atproto_blocks (user_id, cid, data) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, cid) DO NOTHING
	`, bs.userID, b.Cid().String(), b.RawData())
	return err
}
