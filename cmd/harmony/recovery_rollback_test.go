package main

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	blockfactory "github.com/harmony-one/harmony/block/factory"
	"github.com/harmony-one/harmony/core"
	"github.com/harmony-one/harmony/core/types"
	"github.com/stretchr/testify/require"
)

type recoveryRollbackTestChain struct {
	core.Stub
	current        *types.Block
	blocks         map[common.Hash]*types.Block
	rollbackErr    error
	rollbackNoop   bool
	commitWriteErr error
	commitWrites   []uint64
	rollbackCalls  int
}

func (c *recoveryRollbackTestChain) CurrentBlock() *types.Block { return c.current }
func (c *recoveryRollbackTestChain) GetBlock(hash common.Hash, number uint64) *types.Block {
	block := c.blocks[hash]
	if block == nil || block.NumberU64() != number {
		return nil
	}
	return block
}
func (c *recoveryRollbackTestChain) Rollback(hashes []common.Hash) error {
	c.rollbackCalls++
	if c.rollbackErr != nil {
		return c.rollbackErr
	}
	if c.rollbackNoop {
		return nil
	}
	if len(hashes) != 1 || c.current == nil || hashes[0] != c.current.Hash() {
		return errors.New("unexpected rollback request")
	}
	c.current = c.blocks[c.current.ParentHash()]
	return nil
}
func (c *recoveryRollbackTestChain) WriteCommitSig(number uint64, _ []byte) error {
	if c.commitWriteErr != nil {
		return c.commitWriteErr
	}
	c.commitWrites = append(c.commitWrites, number)
	return nil
}

func recoveryRollbackTestBlock(number uint64, parent common.Hash) *types.Block {
	header := blockfactory.ForMainnet.NewHeader(big.NewInt(3002))
	header.SetNumber(new(big.Int).SetUint64(number))
	header.SetParentHash(parent)
	return types.NewBlockWithHeader(header)
}

func TestRollbackEmergencyRecoveryChainRejectsNoProgress(t *testing.T) {
	target := recoveryRollbackTestBlock(10, common.HexToHash("0x01"))
	current := recoveryRollbackTestBlock(11, target.Hash())
	chain := &recoveryRollbackTestChain{
		current:      current,
		blocks:       map[common.Hash]*types.Block{target.Hash(): target},
		rollbackNoop: true,
	}

	err := rollbackEmergencyRecoveryChain(chain, target.NumberU64(), target.Hash(), nil)
	require.ErrorContains(t, err, "made no progress")
	require.Equal(t, 1, chain.rollbackCalls)
	require.Empty(t, chain.commitWrites)
}

func TestRollbackEmergencyRecoveryChainPreflightsAncestry(t *testing.T) {
	target := recoveryRollbackTestBlock(10, common.HexToHash("0x01"))
	current := recoveryRollbackTestBlock(12, common.HexToHash("0xmissing"))
	chain := &recoveryRollbackTestChain{
		current: current,
		blocks:  map[common.Hash]*types.Block{target.Hash(): target},
	}

	err := rollbackEmergencyRecoveryChain(chain, target.NumberU64(), target.Hash(), nil)
	require.ErrorContains(t, err, "rollback ancestry is incomplete")
	require.Zero(t, chain.rollbackCalls)
	require.Empty(t, chain.commitWrites)
}

func TestRollbackEmergencyRecoveryChainMovesToTarget(t *testing.T) {
	target := recoveryRollbackTestBlock(10, common.HexToHash("0x01"))
	middle := recoveryRollbackTestBlock(11, target.Hash())
	current := recoveryRollbackTestBlock(12, middle.Hash())
	chain := &recoveryRollbackTestChain{
		current: current,
		blocks: map[common.Hash]*types.Block{
			target.Hash(): target,
			middle.Hash(): middle,
		},
	}

	require.NoError(t, rollbackEmergencyRecoveryChain(chain, target.NumberU64(), target.Hash(), nil))
	require.Equal(t, target.Hash(), chain.current.Hash())
	require.Equal(t, 2, chain.rollbackCalls)
	require.Empty(t, chain.commitWrites)
}

func TestRollbackEmergencyRecoveryChainRejectsWrongTargetAncestry(t *testing.T) {
	expectedTarget := recoveryRollbackTestBlock(10, common.HexToHash("0x01"))
	wrongTarget := recoveryRollbackTestBlock(10, common.HexToHash("0x02"))
	middle := recoveryRollbackTestBlock(11, wrongTarget.Hash())
	current := recoveryRollbackTestBlock(12, middle.Hash())
	chain := &recoveryRollbackTestChain{
		current: current,
		blocks: map[common.Hash]*types.Block{
			wrongTarget.Hash(): wrongTarget,
			middle.Hash():      middle,
		},
	}

	err := rollbackEmergencyRecoveryChain(chain, expectedTarget.NumberU64(), expectedTarget.Hash(), nil)
	require.ErrorContains(t, err, "does not reach expected target")
	require.Zero(t, chain.rollbackCalls)
	require.Empty(t, chain.commitWrites)
	require.Equal(t, current.Hash(), chain.current.Hash())
}

func TestRollbackEmergencyRecoveryChainPreparesBeforeMovingHead(t *testing.T) {
	target := recoveryRollbackTestBlock(10, common.HexToHash("0x01"))
	current := recoveryRollbackTestBlock(11, target.Hash())
	chain := &recoveryRollbackTestChain{
		current:        current,
		blocks:         map[common.Hash]*types.Block{target.Hash(): target},
		commitWriteErr: errors.New("certificate write failed"),
	}
	prepareCalls := 0
	prepare := func() error {
		prepareCalls++
		return chain.WriteCommitSig(target.NumberU64(), nil)
	}

	err := rollbackEmergencyRecoveryChain(chain, target.NumberU64(), target.Hash(), prepare)
	require.ErrorContains(t, err, "certificate write failed")
	require.Equal(t, 1, prepareCalls)
	require.Zero(t, chain.rollbackCalls)
	require.Empty(t, chain.commitWrites)
	require.Equal(t, current.Hash(), chain.current.Hash())
}
