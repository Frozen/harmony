package vm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/harmony-one/harmony/internal/params"
	stakingTypes "github.com/harmony-one/harmony/staking/types"
)

func TestEmergencyRecoveryStakingPrecompileSelection(t *testing.T) {
	address := common.BytesToAddress([]byte{252})
	tests := []struct {
		name        string
		config      *params.ChainConfig
		shardID     uint32
		blockNumber uint64
		wantFrozen  bool
	}{
		{name: "mainnet shard 0 after target", config: params.MainnetChainConfig, shardID: 0, blockNumber: params.EmergencyRecoveryRetainedBlock + 1, wantFrozen: true},
		{name: "target block", config: params.MainnetChainConfig, shardID: 0, blockNumber: params.EmergencyRecoveryRetainedBlock},
		{name: "other shard", config: params.MainnetChainConfig, shardID: 1, blockNumber: params.EmergencyRecoveryRetainedBlock + 1},
		{name: "other network", config: params.TestnetChainConfig, shardID: 0, blockNumber: params.EmergencyRecoveryRetainedBlock + 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evm := NewEVM(BlockContext{
				BlockNumber: new(big.Int).SetUint64(test.blockNumber),
				EpochNumber: big.NewInt(3002),
				ShardID:     test.shardID,
			}, TxContext{}, nil, test.config, Config{})
			precompile, ok := evm.precompile(address)
			if !ok {
				t.Fatal("staking precompile not selected")
			}
			_, frozen := precompile.(*emergencyRecoveryFrozenStakingPrecompile)
			if frozen != test.wantFrozen {
				t.Fatalf("frozen precompile selected = %v, want %v", frozen, test.wantFrozen)
			}
		})
	}
}

func TestEmergencyRecoveryStakingPrecompileFailsBeforeCallback(t *testing.T) {
	callbackCalled := false
	evm := NewEVM(BlockContext{
		BlockNumber: new(big.Int).SetUint64(params.EmergencyRecoveryRetainedBlock + 1),
		EpochNumber: big.NewInt(3002),
		ShardID:     0,
		Delegate: func(StateDB, RosettaTracer, *stakingTypes.Delegate) error {
			callbackCalled = true
			return nil
		},
	}, TxContext{}, nil, params.MainnetChainConfig, Config{})

	precompile, ok := evm.precompile(emergencyRecoveryStakingPrecompileAddress)
	if !ok {
		t.Fatal("staking precompile not selected")
	}
	contract := NewContract(AccountRef(common.Address{}), AccountRef(emergencyRecoveryStakingPrecompileAddress), new(big.Int), 1)
	if _, _, err := RunPrecompiledContract(precompile, evm, contract, []byte{1, 2, 3, 4}, 1, false); !errors.Is(err, ErrEmergencyRecoveryStakingFrozen) {
		t.Fatalf("RunPrecompiledContract() error = %v, want %v", err, ErrEmergencyRecoveryStakingFrozen)
	}
	if callbackCalled {
		t.Fatal("staking callback was invoked")
	}
	if len(evm.StakeMsgs) != 0 {
		t.Fatalf("staking messages mutated: %d", len(evm.StakeMsgs))
	}
}
