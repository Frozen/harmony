package core

import (
	"errors"
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/harmony-one/harmony/block"
	blockfactory "github.com/harmony-one/harmony/block/factory"
	"github.com/harmony-one/harmony/core/types"
	"github.com/harmony-one/harmony/internal/params"
	staking "github.com/harmony-one/harmony/staking/types"
)

func TestValidateEmergencyRecoveryFrozenPayload(t *testing.T) {
	tests := []struct {
		name  string
		block *types.Block
	}{
		{name: "staking transaction", block: emergencyRecoveryBlockWithStaking(t)},
		{name: "incoming receipt", block: emergencyRecoveryBlockWithIncomingReceipt()},
		{name: "incoming receipt commitment", block: emergencyRecoveryBlockWithIncomingReceiptCommitment()},
		{name: "crosslink", block: emergencyRecoveryBlockWithCrossLink()},
		{name: "slash", block: emergencyRecoveryBlockWithSlash()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateEmergencyRecoveryFrozenPayload(params.MainnetChainConfig, test.block); !errors.Is(err, ErrEmergencyRecoveryFeatureFrozen) {
				t.Fatalf("ValidateEmergencyRecoveryFrozenPayload() error = %v, want %v", err, ErrEmergencyRecoveryFeatureFrozen)
			}
		})
	}
}

func TestEmergencyRecoveryFeatureFreezeScope(t *testing.T) {
	bad := emergencyRecoveryBlockWithSlash()
	if err := ValidateEmergencyRecoveryFrozenPayload(params.MainnetChainConfig, emergencyRecoveryEmptyBlock(params.EmergencyRecoveryRetainedBlock, 0)); err != nil {
		t.Fatalf("target block unexpectedly frozen: %v", err)
	}
	if err := ValidateEmergencyRecoveryFrozenPayload(params.MainnetChainConfig, emergencyRecoveryEmptyBlock(params.EmergencyRecoveryRetainedBlock+1, 1)); err != nil {
		t.Fatalf("shard 1 unexpectedly frozen: %v", err)
	}
	if err := ValidateEmergencyRecoveryFrozenPayload(params.TestnetChainConfig, bad); err != nil {
		t.Fatalf("testnet unexpectedly frozen: %v", err)
	}
	if err := ValidateEmergencyRecoveryFrozenPayload(params.MainnetChainConfig, emergencyRecoveryEmptyBlock(params.EmergencyRecoveryRetainedBlock+1, 0)); err != nil {
		t.Fatalf("empty recovery block rejected: %v", err)
	}
}

func TestEmergencyRecoveryDerivedStakingMessagesFrozen(t *testing.T) {
	block := emergencyRecoveryEmptyBlock(params.EmergencyRecoveryRetainedBlock+1, 0)
	if err := validateEmergencyRecoveryDerivedStaking(params.MainnetChainConfig, block, 1); !errors.Is(err, ErrEmergencyRecoveryFeatureFrozen) {
		t.Fatalf("validateEmergencyRecoveryDerivedStaking() error = %v, want %v", err, ErrEmergencyRecoveryFeatureFrozen)
	}
	if err := validateEmergencyRecoveryDerivedStaking(params.MainnetChainConfig, block, 0); err != nil {
		t.Fatalf("empty derived staking messages rejected: %v", err)
	}
}

func TestEmergencyRecoveryViewIDPolicy(t *testing.T) {
	beforeTarget := emergencyRecoveryEmptyBlock(params.EmergencyRecoveryRetainedBlock, 0)
	if err := validateEmergencyRecoveryViewIDWithFloor(params.MainnetChainConfig, beforeTarget, 100); err != nil {
		t.Fatalf("target block unexpectedly subject to new-block ViewID policy: %v", err)
	}

	header := emergencyRecoveryHeader(params.EmergencyRecoveryRetainedBlock+1, 0)
	header.SetViewID(big.NewInt(99))
	block := types.NewBlockWithHeader(header)
	if err := validateEmergencyRecoveryViewIDWithFloor(params.MainnetChainConfig, block, 100); !errors.Is(err, ErrEmergencyRecoveryViewIDBelowFloor) {
		t.Fatalf("below-floor ViewID error = %v, want %v", err, ErrEmergencyRecoveryViewIDBelowFloor)
	}

	header.SetViewID(big.NewInt(100))
	if err := validateEmergencyRecoveryViewIDWithFloor(params.MainnetChainConfig, types.NewBlockWithHeader(header), 100); err != nil {
		t.Fatalf("floor ViewID rejected: %v", err)
	}
	if err := validateEmergencyRecoveryViewIDWithFloor(params.MainnetChainConfig, types.NewBlockWithHeader(header), 0); !errors.Is(err, ErrEmergencyRecoveryViewIDFloorUnset) {
		t.Fatalf("unset floor error = %v, want %v", err, ErrEmergencyRecoveryViewIDFloorUnset)
	}
	header.SetViewID(new(big.Int).SetUint64(math.MaxUint64))
	if err := validateEmergencyRecoveryViewIDWithFloor(params.MainnetChainConfig, types.NewBlockWithHeader(header), 100); !errors.Is(err, ErrEmergencyRecoveryViewIDInvalid) {
		t.Fatalf("exhausted ViewID error = %v, want %v", err, ErrEmergencyRecoveryViewIDInvalid)
	}
}

func TestEmergencyRecoveryViewIDPolicyRunsBeforeKnownAndWriteShortcuts(t *testing.T) {
	block := emergencyRecoveryEmptyBlock(params.EmergencyRecoveryRetainedBlock+1, 0)
	want := ErrEmergencyRecoveryViewIDBelowFloor
	if params.EmergencyRecoveryViewIDFloor == 0 {
		want = ErrEmergencyRecoveryViewIDFloorUnset
	}

	chain := emergencyRecoveryKnownChain{config: params.MainnetChainConfig}
	if err := NewBlockValidator(chain).ValidateBody(block); !errors.Is(err, want) {
		t.Fatalf("ValidateBody() error = %v, want %v before known-block shortcut", err, want)
	}

	directChain := &BlockChainImpl{chainConfig: params.MainnetChainConfig}
	if err := directChain.WriteBlockWithoutState(block); !errors.Is(err, want) {
		t.Fatalf("WriteBlockWithoutState() error = %v, want %v before database access", err, want)
	}

	headerChain := &HeaderChain{config: params.MainnetChainConfig}
	if _, err := headerChain.WriteHeader(block.Header()); !errors.Is(err, want) {
		t.Fatalf("WriteHeader() error = %v, want %v before database access", err, want)
	}
}

type emergencyRecoveryKnownChain struct {
	Stub
	config *params.ChainConfig
}

func (c emergencyRecoveryKnownChain) Config() *params.ChainConfig { return c.config }

func (emergencyRecoveryKnownChain) HasBlockAndState(common.Hash, uint64) bool { return true }

func TestEmergencyRecoveryFreezeRunsBeforeKnownBlockShortcut(t *testing.T) {
	chain := emergencyRecoveryKnownChain{config: params.MainnetChainConfig}
	err := NewBlockValidator(chain).ValidateBody(emergencyRecoveryBlockWithSlash())
	if !errors.Is(err, ErrEmergencyRecoveryFeatureFrozen) {
		t.Fatalf("ValidateBody() error = %v, want freeze error before known-block shortcut", err)
	}
}

func TestEmergencyRecoveryFreezeRunsBeforeDirectDatabaseWrites(t *testing.T) {
	chain := &BlockChainImpl{chainConfig: params.MainnetChainConfig}
	block := emergencyRecoveryBlockWithSlash()

	if err := chain.WriteBlockWithoutState(block); !errors.Is(err, ErrEmergencyRecoveryFeatureFrozen) {
		t.Fatalf("WriteBlockWithoutState() error = %v, want freeze error", err)
	}
	if _, err := chain.InsertChain(types.Blocks{block}, false); !errors.Is(err, ErrEmergencyRecoveryFeatureFrozen) {
		t.Fatalf("InsertChain() error = %v, want freeze error", err)
	}
}

func emergencyRecoveryHeader(number uint64, shardID uint32) *block.Header {
	return blockfactory.NewTestHeader().With().
		Number(new(big.Int).SetUint64(number)).
		ShardID(shardID).
		Header()
}

func emergencyRecoveryEmptyBlock(number uint64, shardID uint32) *types.Block {
	return types.NewBlock(emergencyRecoveryHeader(number, shardID), nil, nil, nil, nil, nil)
}

func emergencyRecoveryBlockWithStaking(t *testing.T) *types.Block {
	t.Helper()
	stx, err := staking.NewStakingTransaction(0, 21000, big.NewInt(1), func() (staking.Directive, interface{}) {
		return staking.DirectiveCollectRewards, staking.CollectRewards{}
	})
	if err != nil {
		t.Fatal(err)
	}
	return types.NewBlock(
		emergencyRecoveryHeader(params.EmergencyRecoveryRetainedBlock+1, 0),
		nil, []*types.Receipt{types.NewReceipt(nil, false, 0)}, nil, nil,
		[]*staking.StakingTransaction{stx},
	)
}

func emergencyRecoveryBlockWithIncomingReceipt() *types.Block {
	proof := &types.CXReceiptsProof{
		MerkleProof: &types.CXMerkleProof{BlockNum: big.NewInt(1)},
		Header:      blockfactory.NewTestHeader(),
	}
	return types.NewBlock(
		emergencyRecoveryHeader(params.EmergencyRecoveryRetainedBlock+1, 0),
		nil, nil, nil, []*types.CXReceiptsProof{proof}, nil,
	)
}

func emergencyRecoveryBlockWithIncomingReceiptCommitment() *types.Block {
	header := emergencyRecoveryHeader(params.EmergencyRecoveryRetainedBlock+1, 0)
	header.SetIncomingReceiptHash(common.HexToHash("0x01"))
	return types.NewBlockWithHeader(header)
}

func emergencyRecoveryBlockWithCrossLink() *types.Block {
	header := emergencyRecoveryHeader(params.EmergencyRecoveryRetainedBlock+1, 0)
	header.SetCrossLinks([]byte{1})
	return types.NewBlock(header, nil, nil, nil, nil, nil)
}

func emergencyRecoveryBlockWithSlash() *types.Block {
	header := emergencyRecoveryHeader(params.EmergencyRecoveryRetainedBlock+1, 0)
	header.SetSlashes([]byte{1})
	return types.NewBlock(header, nil, nil, nil, nil, nil)
}
