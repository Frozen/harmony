package consensus

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/harmony-one/harmony/block"
	blockfactory "github.com/harmony-one/harmony/block/factory"
	consensusengine "github.com/harmony-one/harmony/consensus/engine"
	"github.com/harmony-one/harmony/core"
	"github.com/harmony-one/harmony/core/rawdb"
	"github.com/harmony-one/harmony/core/state"
	"github.com/harmony-one/harmony/core/types"
	"github.com/harmony-one/harmony/internal/params"
	"github.com/stretchr/testify/require"
)

type recoveryCheckpointTestChain struct {
	core.Stub
	current   *types.Block
	fast      *types.Block
	blocks    map[common.Hash]*types.Block
	canonical map[uint64]common.Hash
	stateErr  error
}

func (c *recoveryCheckpointTestChain) ShardID() uint32 { return 0 }
func (c *recoveryCheckpointTestChain) Config() *params.ChainConfig {
	return params.MainnetChainConfig
}
func (c *recoveryCheckpointTestChain) CurrentBlock() *types.Block { return c.current }
func (c *recoveryCheckpointTestChain) CurrentFastBlock() *types.Block {
	return c.fast
}
func (c *recoveryCheckpointTestChain) CurrentHeader() *block.Header {
	if c.current == nil {
		return nil
	}
	return c.current.Header()
}
func (c *recoveryCheckpointTestChain) GetCanonicalHash(number uint64) common.Hash {
	return c.canonical[number]
}
func (c *recoveryCheckpointTestChain) GetBlock(hash common.Hash, number uint64) *types.Block {
	result := c.blocks[hash]
	if result == nil || result.NumberU64() != number {
		return nil
	}
	return result
}
func (c *recoveryCheckpointTestChain) StateAt(common.Hash) (*state.DB, error) {
	if c.stateErr != nil {
		return nil, c.stateErr
	}
	return new(state.DB), nil
}

func recoveryCheckpointTestBlock(number uint64, parent, root common.Hash) *types.Block {
	header := blockfactory.ForMainnet.NewHeader(big.NewInt(3002))
	header.SetNumber(new(big.Int).SetUint64(number))
	header.SetShardID(0)
	header.SetParentHash(parent)
	header.SetRoot(root)
	header.SetViewID(new(big.Int).SetUint64(number + 100))
	return types.NewBlockWithHeader(header)
}

func newRecoveryCheckpointTestChain() (*recoveryCheckpointTestChain, *types.Block, *types.Block) {
	targetRoot := common.HexToHash("0x1234")
	target := recoveryCheckpointTestBlock(EmergencyRecoveryRetainedBlock, common.HexToHash("0xabcd"), targetRoot)
	descendant := recoveryCheckpointTestBlock(EmergencyRecoveryRetainedBlock+1, target.Hash(), common.HexToHash("0x5678"))
	chain := &recoveryCheckpointTestChain{
		current: descendant,
		fast:    descendant,
		blocks: map[common.Hash]*types.Block{
			target.Hash():     target,
			descendant.Hash(): descendant,
		},
		canonical: map[uint64]common.Hash{
			EmergencyRecoveryRetainedBlock:     target.Hash(),
			EmergencyRecoveryRetainedBlock + 1: descendant.Hash(),
		},
	}
	return chain, target, descendant
}

func TestEmergencyRecoveryCheckpointReleaseTuple(t *testing.T) {
	hash, root, err := EmergencyRecoveryCheckpoint()
	require.NoError(t, err)
	require.Equal(t,
		common.HexToHash("0x30c35d2f2291e4b27debe7862956cf7a0cc7abefc044273d6823567335086d8d"),
		hash,
	)
	require.Equal(t,
		common.HexToHash("0x39e72dc20835abe61f69966bec2cc4766bb9e893c4168e117154dd539f2fc728"),
		root,
	)
}

func TestEmergencyRecoveryCheckpointAcceptsPinnedAncestry(t *testing.T) {
	chain, target, _ := newRecoveryCheckpointTestChain()
	require.NoError(t, validateEmergencyRecoveryCheckpointWith(
		chain, target.Hash(), target.Root(), func(common.Hash) error { return nil },
	))
}

func TestEmergencyRecoveryCheckpointValidatesPersistedHeads(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	expected := common.HexToHash("0x1234")
	require.NoError(t, rawdb.WriteHeadHeaderHash(db, expected))
	require.NoError(t, rawdb.WriteHeadFastBlockHash(db, expected))
	require.NoError(t, rawdb.WriteHeadBlockHash(db, expected))
	require.NoError(t, validateEmergencyRecoveryPersistedHeads(db, expected))

	require.NoError(t, rawdb.WriteHeadFastBlockHash(db, common.HexToHash("0xdead")))
	require.ErrorIs(t,
		validateEmergencyRecoveryPersistedHeads(db, expected),
		ErrEmergencyRecoveryCheckpointMismatch,
	)
}

func TestEmergencyRecoveryCheckpointFailsClosed(t *testing.T) {
	chain, target, descendant := newRecoveryCheckpointTestChain()

	t.Run("divergent fast head", func(t *testing.T) {
		copyChain := *chain
		copyChain.fast = target
		require.ErrorIs(t, validateEmergencyRecoveryCheckpointWith(
			&copyChain, target.Hash(), target.Root(), func(common.Hash) error { return nil },
		), ErrEmergencyRecoveryCheckpointMismatch)
	})

	t.Run("wrong target canonical hash", func(t *testing.T) {
		copyChain := *chain
		copyChain.canonical = map[uint64]common.Hash{
			EmergencyRecoveryRetainedBlock:     common.HexToHash("0xdead"),
			EmergencyRecoveryRetainedBlock + 1: descendant.Hash(),
		}
		require.ErrorIs(t, validateEmergencyRecoveryCheckpointWith(
			&copyChain, target.Hash(), target.Root(), func(common.Hash) error { return nil },
		), ErrEmergencyRecoveryCheckpointMismatch)
	})

	t.Run("missing parent", func(t *testing.T) {
		copyChain := *chain
		copyChain.blocks = map[common.Hash]*types.Block{descendant.Hash(): descendant}
		require.ErrorIs(t, validateEmergencyRecoveryCheckpointWith(
			&copyChain, target.Hash(), target.Root(), func(common.Hash) error { return nil },
		), ErrEmergencyRecoveryCheckpointMismatch)
	})

	t.Run("wrong target root", func(t *testing.T) {
		require.ErrorIs(t, validateEmergencyRecoveryCheckpointWith(
			chain, target.Hash(), common.HexToHash("0xbad"), func(common.Hash) error { return nil },
		), ErrEmergencyRecoveryCheckpointMismatch)
	})

	t.Run("unavailable current state", func(t *testing.T) {
		copyChain := *chain
		copyChain.stateErr = errors.New("missing trie node")
		require.ErrorIs(t, validateEmergencyRecoveryCheckpointWith(
			&copyChain, target.Hash(), target.Root(), func(common.Hash) error { return nil },
		), ErrEmergencyRecoveryCheckpointMismatch)
	})

	t.Run("rejected descendant", func(t *testing.T) {
		require.ErrorIs(t, validateEmergencyRecoveryCheckpointWith(
			chain, target.Hash(), target.Root(), func(hash common.Hash) error {
				if hash == descendant.Hash() {
					return consensusengine.ErrRejectedBlock
				}
				return nil
			},
		), consensusengine.ErrRejectedBlock)
	})
}
