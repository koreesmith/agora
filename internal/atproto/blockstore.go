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
type pgBlockstore struct {
	db     *store.DB
	userID string
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
	_, err := bs.db.ExecContext(ctx, `
		INSERT INTO atproto_blocks (user_id, cid, data) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, cid) DO NOTHING
	`, bs.userID, b.Cid().String(), b.RawData())
	return err
}
