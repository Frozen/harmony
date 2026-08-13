package core

import (
	"math/big"
	"testing"

	"github.com/harmony-one/harmony/core/types"
	"github.com/harmony-one/harmony/internal/params"
)

func TestEmergencyRecoverySkipsLatestCrossLinkRollup(t *testing.T) {
	activeHeader := emergencyRecoveryHeader(params.EmergencyRecoveryRetainedBlock, 0)
	activeHeader.SetEpoch(new(big.Int).Set(params.MainnetChainConfig.CrossLinkEpoch))
	activeHead := types.NewBlockWithHeader(activeHeader)

	mainnet := &BlockChainImpl{chainConfig: params.MainnetChainConfig}
	mainnet.currentBlock.Store(activeHead)
	if mainnet.shouldRollUpLatestCrossLinks(
		emergencyRecoveryEmptyBlock(params.EmergencyRecoveryRetainedBlock+1, 0), true,
	) {
		t.Fatal("post-target mainnet recovery block must not roll up stale crosslinks")
	}
	if !mainnet.shouldRollUpLatestCrossLinks(
		emergencyRecoveryEmptyBlock(params.EmergencyRecoveryRetainedBlock, 0), true,
	) {
		t.Fatal("retained mainnet history unexpectedly skips crosslink rollup")
	}

	testnet := &BlockChainImpl{chainConfig: params.TestnetChainConfig}
	testnet.currentBlock.Store(activeHead)
	if !testnet.shouldRollUpLatestCrossLinks(
		emergencyRecoveryEmptyBlock(params.EmergencyRecoveryRetainedBlock+1, 0), true,
	) {
		t.Fatal("testnet unexpectedly skips crosslink rollup at mainnet recovery height")
	}
	if mainnet.shouldRollUpLatestCrossLinks(
		emergencyRecoveryEmptyBlock(params.EmergencyRecoveryRetainedBlock, 0), false,
	) {
		t.Fatal("non-beacon chain unexpectedly rolls up crosslinks")
	}
}
