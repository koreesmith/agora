package atproto

import (
	"io"

	"github.com/ipfs/go-cid"
	car "github.com/ipld/go-car"
	carutil "github.com/ipld/go-car/util"
)

// writeCommitCAR encodes a flat set of blocks as a CARv1 file with root set
// to the commit CID, per the firehose #commit event's "blocks" field spec:
// "the commit must be included as a block, and the commit block CID must be
// the first entry in the CAR header 'roots' list." Unlike car.WriteCar (which
// walks a DAG from its roots via a NodeGetter), this just writes the exact
// block set already known from the blockstore's recording (AGORA-191) — a
// single commit's writes are already precisely the diff, no walk needed.
func writeCommitCAR(w io.Writer, commitCid cid.Cid, blocks map[string][]byte) error {
	if err := car.WriteHeader(&car.CarHeader{Roots: []cid.Cid{commitCid}, Version: 1}, w); err != nil {
		return err
	}
	for cidStr, data := range blocks {
		c, err := cid.Decode(cidStr)
		if err != nil {
			return err
		}
		if err := carutil.LdWrite(w, c.Bytes(), data); err != nil {
			return err
		}
	}
	return nil
}
