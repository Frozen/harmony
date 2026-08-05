package harmony

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	blockfactory "github.com/harmony-one/harmony/block/factory"
	"github.com/harmony-one/harmony/consensus/hotstuff"
	coretypes "github.com/harmony-one/harmony/core/types"
	hmybls "github.com/harmony-one/harmony/crypto/bls"
	"github.com/stretchr/testify/require"
)

func TestNewBlockMapsHarmonyHeaderFromVerifiedQC(t *testing.T) {
	parentHash := common.HexToHash("0x1234")
	header := blockfactory.NewTestHeader()
	header.SetParentHash(parentHash)
	header.SetViewID(big.NewInt(7))
	block := coretypes.NewBlockWithHeader(header)
	verified := verifiedQC(t, hotstuff.BlockID(parentHash.Hex()), 0)
	justify := verified.QC()

	proposal, err := NewBlock(block, verified)
	require.NoError(t, err)
	require.Equal(t, hotstuff.BlockID(block.Hash().Hex()), proposal.ID)
	require.Equal(t, hotstuff.BlockID(parentHash.Hex()), proposal.Parent)
	require.Equal(t, hotstuff.View(7), proposal.View)
	require.Equal(t, justify, proposal.Justify)
}

func TestNewBlockRejectsNilHarmonyBlock(t *testing.T) {
	_, err := NewBlock(nil, hotstuff.VerifiedQC{})
	require.ErrorIs(t, err, ErrNilBlock)
}

func TestNewBlockRejectsMissingHarmonyHeader(t *testing.T) {
	_, err := NewBlock(new(coretypes.Block), hotstuff.VerifiedQC{})
	require.ErrorIs(t, err, ErrNilBlockHeader)
}

func TestNewBlockRejectsViewOutsideUint64(t *testing.T) {
	header := blockfactory.NewTestHeader()
	header.SetViewID(new(big.Int).Lsh(big.NewInt(1), 64))

	_, err := NewBlock(coretypes.NewBlockWithHeader(header), hotstuff.VerifiedQC{})
	require.ErrorIs(t, err, ErrViewOverflow)
}

func verifiedQC(t *testing.T, blockID hotstuff.BlockID, view hotstuff.View) hotstuff.VerifiedQC {
	t.Helper()
	key := hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey())
	committee, err := hotstuff.NewBLSCommitteeFromValidatedKeys([]hotstuff.BLSMember{{
		Member:    hotstuff.Member{ID: "validator-a", Power: 1},
		PublicKey: *key.Pub,
	}})
	require.NoError(t, err)
	authority := hotstuff.NewQCAuthority(committee, hotstuff.VoteDomain{Genesis: blockID})
	_, verified, err := authority.NewCore(hotstuff.Block{ID: blockID, View: view})
	require.NoError(t, err)
	return verified
}
