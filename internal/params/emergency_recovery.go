package params

// EmergencyRecoveryRetainedBlock is the last canonical shard-0 block retained
// by the emergency recovery release.
const EmergencyRecoveryRetainedBlock uint64 = 92730034

// EmergencyRecoveryViewIDFloor is the recovery release's signed activation
// floor for mainnet shard 0.
const EmergencyRecoveryViewIDFloor uint64 = 1_000_000_000

// IsEmergencyRecoveryFeatureFreeze reports whether features backed by
// post-target auxiliary metadata must remain disabled. Shard 0 is the beacon
// chain; keeping this helper in params avoids duplicating the activation height
// between core block validation and the EVM staking precompile.
func IsEmergencyRecoveryFeatureFreeze(config *ChainConfig, shardID uint32, blockNumber uint64) bool {
	return config != nil && config.ChainID != nil &&
		config.ChainID.Cmp(MainnetChainID) == 0 &&
		shardID == 0 && blockNumber > EmergencyRecoveryRetainedBlock
}
