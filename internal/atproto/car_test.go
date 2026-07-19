package atproto

import (
	"bytes"
	"testing"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	car "github.com/ipld/go-car"
)

// AGORA-231: writeCAR backs both GetRepo's full-repo export and GetBlocks'
// arbitrary-block export, so a byte-format regression here would silently
// break every relay's backfill, not surface as a Go compile error.
func TestWriteCAR(t *testing.T) {
	b1 := blocks.NewBlock([]byte("block one"))
	b2 := blocks.NewBlock([]byte("block two"))
	data := map[string][]byte{
		b1.Cid().String(): b1.RawData(),
		b2.Cid().String(): b2.RawData(),
	}

	cases := []struct {
		name  string
		roots []cid.Cid
	}{
		{name: "single root (GetRepo shape)", roots: []cid.Cid{b1.Cid()}},
		{name: "multiple roots (GetBlocks shape)", roots: []cid.Cid{b1.Cid(), b2.Cid()}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := writeCAR(&buf, tc.roots, data); err != nil {
				t.Fatalf("writeCAR: %v", err)
			}

			cr, err := car.NewCarReader(&buf)
			if err != nil {
				t.Fatalf("NewCarReader: %v", err)
			}
			if len(cr.Header.Roots) != len(tc.roots) {
				t.Errorf("roots = %v, want %v", cr.Header.Roots, tc.roots)
			}

			got := make(map[string][]byte)
			for {
				blk, err := cr.Next()
				if err != nil {
					break
				}
				got[blk.Cid().String()] = blk.RawData()
			}
			if len(got) != len(data) {
				t.Fatalf("decoded %d blocks, want %d", len(got), len(data))
			}
			for k, v := range data {
				if !bytes.Equal(got[k], v) {
					t.Errorf("block %s = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// writeCommitCAR is writeCAR's single-root special case (the shape emitCommit
// needs) — confirm it actually produces that shape rather than drifting.
func TestWriteCommitCAR(t *testing.T) {
	b := blocks.NewBlock([]byte("the commit block"))
	data := map[string][]byte{b.Cid().String(): b.RawData()}

	var buf bytes.Buffer
	if err := writeCommitCAR(&buf, b.Cid(), data); err != nil {
		t.Fatalf("writeCommitCAR: %v", err)
	}

	cr, err := car.NewCarReader(&buf)
	if err != nil {
		t.Fatalf("NewCarReader: %v", err)
	}
	if len(cr.Header.Roots) != 1 || !cr.Header.Roots[0].Equals(b.Cid()) {
		t.Errorf("roots = %v, want [%v]", cr.Header.Roots, b.Cid())
	}
}
