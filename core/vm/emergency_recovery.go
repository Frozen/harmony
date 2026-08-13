package vm

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/harmony-one/harmony/internal/params"
)

var (
	emergencyRecoveryStakingPrecompileAddress = common.BytesToAddress([]byte{252})
	// ErrEmergencyRecoveryStakingFrozen is returned by calls to the staking
	// precompile while post-target validator metadata is quarantined.
	ErrEmergencyRecoveryStakingFrozen = errors.New("staking precompile is frozen during emergency recovery")
	emergencyRecoveryFrozenStaking    = &emergencyRecoveryFrozenStakingPrecompile{}
)

func isEmergencyRecoveryStakingPrecompileFrozen(evm *EVM, addr common.Address) bool {
	if evm == nil || evm.Context.BlockNumber == nil || evm.Context.BlockNumber.Sign() < 0 ||
		evm.Context.BlockNumber.BitLen() > 64 || addr != emergencyRecoveryStakingPrecompileAddress {
		return false
	}
	return params.IsEmergencyRecoveryFeatureFreeze(
		evm.ChainConfig(), evm.Context.ShardID, evm.Context.BlockNumber.Uint64(),
	)
}

// emergencyRecoveryFrozenStakingPrecompile preserves 0xfc as a precompile but
// makes every write attempt fail before parsing input or invoking any staking
// callback. The EVM reverts the call snapshot and consumes the call gas.
type emergencyRecoveryFrozenStakingPrecompile struct{}

var _ WriteCapablePrecompiledContract = (*emergencyRecoveryFrozenStakingPrecompile)(nil)

func (*emergencyRecoveryFrozenStakingPrecompile) IsWrite() bool { return true }

func (*emergencyRecoveryFrozenStakingPrecompile) RequiredGas(*EVM, *Contract, []byte) (uint64, error) {
	return 0, ErrEmergencyRecoveryStakingFrozen
}

func (*emergencyRecoveryFrozenStakingPrecompile) RunWriteCapable(*EVM, *Contract, []byte) ([]byte, error) {
	return nil, ErrEmergencyRecoveryStakingFrozen
}
