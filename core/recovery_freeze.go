package core

import (
	"errors"
	"fmt"
	"math"

	"github.com/harmony-one/harmony/core/types"
	"github.com/harmony-one/harmony/internal/params"
)

var (
	ErrEmergencyRecoveryFeatureFrozen    = errors.New("feature is frozen during emergency recovery")
	ErrEmergencyRecoveryViewIDFloorUnset = errors.New("emergency recovery ViewID floor is unset")
	ErrEmergencyRecoveryViewIDBelowFloor = errors.New("block ViewID is below emergency recovery floor")
	ErrEmergencyRecoveryViewIDInvalid    = errors.New("invalid emergency recovery block ViewID")
)

func isEmergencyRecoveryBlock(config *params.ChainConfig, block *types.Block) bool {
	return block != nil && block.Header() != nil &&
		params.IsEmergencyRecoveryFeatureFreeze(config, block.ShardID(), block.NumberU64())
}

// ValidateEmergencyRecoveryFrozenPayload blocks all auxiliary data whose
// post-target LevelDB indexes are intentionally left dirty by stock Rollback.
// This release keeps the freeze permanent; a later metadata-cleanup release is
// required before these features can be re-enabled.
func ValidateEmergencyRecoveryFrozenPayload(config *params.ChainConfig, block *types.Block) error {
	if !isEmergencyRecoveryBlock(config, block) {
		return nil
	}
	header := block.Header()
	switch {
	case len(block.StakingTransactions()) != 0:
		return fmt.Errorf("%w: staking transactions", ErrEmergencyRecoveryFeatureFrozen)
	case len(block.IncomingReceipts()) != 0:
		return fmt.Errorf("%w: incoming receipts", ErrEmergencyRecoveryFeatureFrozen)
	case header.IncomingReceiptHash() != types.EmptyRootHash:
		return fmt.Errorf("%w: incoming receipt commitment", ErrEmergencyRecoveryFeatureFrozen)
	case len(header.CrossLinks()) != 0:
		return fmt.Errorf("%w: crosslinks", ErrEmergencyRecoveryFeatureFrozen)
	case len(header.Slashes()) != 0:
		return fmt.Errorf("%w: slashes", ErrEmergencyRecoveryFeatureFrozen)
	}
	return nil
}

func validateEmergencyRecoveryViewIDWithFloor(config *params.ChainConfig, block *types.Block, floor uint64) error {
	if !isEmergencyRecoveryBlock(config, block) {
		return nil
	}
	if floor == 0 {
		return ErrEmergencyRecoveryViewIDFloorUnset
	}
	if floor == math.MaxUint64 {
		return fmt.Errorf("%w: floor exhausts uint64", ErrEmergencyRecoveryViewIDInvalid)
	}
	viewID := block.Header().ViewID()
	if viewID == nil || viewID.Sign() < 0 || viewID.BitLen() > 64 {
		return ErrEmergencyRecoveryViewIDInvalid
	}
	if viewID.Uint64() == math.MaxUint64 {
		return fmt.Errorf("%w: block ViewID exhausts uint64", ErrEmergencyRecoveryViewIDInvalid)
	}
	if viewID.Uint64() < floor {
		return fmt.Errorf("%w: got %s, floor %d", ErrEmergencyRecoveryViewIDBelowFloor, viewID, floor)
	}
	return nil
}

// ValidateEmergencyRecoveryBlockPolicy enforces the complete post-target
// recovery policy before any known-block or database-write shortcut.
func ValidateEmergencyRecoveryBlockPolicy(config *params.ChainConfig, block *types.Block) error {
	if err := ValidateEmergencyRecoveryFrozenPayload(config, block); err != nil {
		return err
	}
	return validateEmergencyRecoveryViewIDWithFloor(config, block, params.EmergencyRecoveryViewIDFloor)
}

func validateEmergencyRecoveryDerivedStaking(config *params.ChainConfig, block *types.Block, stakeMessageCount int) error {
	if isEmergencyRecoveryBlock(config, block) && stakeMessageCount != 0 {
		return fmt.Errorf("%w: EVM-derived staking messages", ErrEmergencyRecoveryFeatureFrozen)
	}
	return nil
}

// IsEmergencyRecoveryFeatureFreeze reports whether proposal-side staking/CX/
// crosslink/slash sources must remain empty.
func IsEmergencyRecoveryFeatureFreeze(config *params.ChainConfig, shardID uint32, blockNumber uint64) bool {
	return params.IsEmergencyRecoveryFeatureFreeze(config, shardID, blockNumber)
}
