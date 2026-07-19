package atproto

import (
	"io"

	"github.com/ipfs/go-cid"
	car "github.com/ipld/go-car"
	carutil "github.com/ipld/go-car/util"
)

// writeCAR encodes a flat set of blocks as a CARv1 file with the given
// roots. Unlike car.WriteCar (which walks a DAG from its roots via a
// NodeGetter), this just writes an already-known block set verbatim — every
// caller here already has exactly the blocks it means to send, no walk
// needed: a single commit's recorded writes (writeCommitCAR), a full repo
// export (GetRepo, AGORA-231), or an arbitrary requested set with no
// meaningful root at all (GetBlocks, AGORA-231 — an empty roots list is
// valid CARv1).
func writeCAR(w io.Writer, roots []cid.Cid, blocks map[string][]byte) error {
	if err := car.WriteHeader(&car.CarHeader{Roots: roots, Version: 1}, w); err != nil {
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

// writeCommitCAR is writeCAR with the single-root shape the firehose
// #commit event's "blocks" field spec requires: "the commit must be
// included as a block, and the commit block CID must be the first entry in
// the CAR header 'roots' list."
func writeCommitCAR(w io.Writer, commitCid cid.Cid, blocks map[string][]byte) error {
	return writeCAR(w, []cid.Cid{commitCid}, blocks)
}
