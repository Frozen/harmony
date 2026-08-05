package harmony

import (
	"errors"

	"github.com/harmony-one/harmony/consensus/hotstuff"
	coretypes "github.com/harmony-one/harmony/core/types"
)

var (
	// ErrNilBlock indicates that no Harmony block was supplied.
	ErrNilBlock = errors.New("hotstuff Harmony block is nil")
	// ErrNilBlockHeader indicates that the supplied Harmony block has no header.
	ErrNilBlockHeader = errors.New("hotstuff Harmony block header is nil")
	// ErrViewOverflow indicates that the Harmony view cannot be represented by the HotStuff core.
	ErrViewOverflow = errors.New("hotstuff Harmony block view does not fit uint64")
)

// NewBlock maps a Harmony block and authority-verified justify QC into the
// immutable structural value accepted by the HotStuff core.
func NewBlock(source *coretypes.Block, verified hotstuff.VerifiedQC) (hotstuff.Block, error) {
	if source == nil {
		return hotstuff.Block{}, ErrNilBlock
	}
	header := source.Header()
	if header == nil {
		return hotstuff.Block{}, ErrNilBlockHeader
	}
	viewID := header.ViewID()
	if !viewID.IsUint64() {
		return hotstuff.Block{}, ErrViewOverflow
	}
	return hotstuff.Block{
		ID:      hotstuff.BlockID(source.Hash().Hex()),
		Parent:  hotstuff.BlockID(source.ParentHash().Hex()),
		View:    hotstuff.View(viewID.Uint64()),
		Justify: verified.QC(),
	}, nil
}
