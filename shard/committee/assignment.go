package committee

import (
	"encoding/json"
	"math/big"
	"sort"

	"github.com/harmony-one/harmony/core/state"
	"github.com/harmony-one/harmony/internal/utils/pseudorandom"

	"github.com/harmony-one/harmony/crypto/bls"

	"github.com/ethereum/go-ethereum/common"
	bls_core "github.com/harmony-one/bls/ffi/go/bls"
	"github.com/harmony-one/harmony/block"
	"github.com/harmony-one/harmony/core/types"
	common2 "github.com/harmony-one/harmony/internal/common"
	nodeconfig "github.com/harmony-one/harmony/internal/configs/node"
	shardingconfig "github.com/harmony-one/harmony/internal/configs/sharding"
	"github.com/harmony-one/harmony/internal/params"
	"github.com/harmony-one/harmony/internal/utils"
	"github.com/harmony-one/harmony/numeric"
	"github.com/harmony-one/harmony/shard"
	"github.com/harmony-one/harmony/staking/availability"
	"github.com/harmony-one/harmony/staking/effective"
	staking "github.com/harmony-one/harmony/staking/types"
	"github.com/pkg/errors"
)

// ValidatorListProvider ..
type ValidatorListProvider interface {
	Compute(
		epoch *big.Int, reader DataProvider,
	) (*shard.State, error)
	ReadFromDB(epoch *big.Int, reader DataProvider) (*shard.State, error)
}

// Reader is committee.Reader and it is the API that committee membership assignment needs
type Reader interface {
	ValidatorListProvider
}

// StakingCandidatesReader ..
type StakingCandidatesReader interface {
	CurrentBlock() *types.Block
	StateAt(root common.Hash) (*state.DB, error)
	ReadValidatorInformation(addr common.Address) (*staking.ValidatorWrapper, error)
	ReadValidatorInformationAtState(
		addr common.Address, state *state.DB,
	) (*staking.ValidatorWrapper, error)
	ReadValidatorSnapshot(addr common.Address) (*staking.ValidatorSnapshot, error)
	ValidatorCandidates() []common.Address
}

// CandidatesForEPoS ..
type CandidatesForEPoS struct {
	Orders                             map[common.Address]effective.SlotOrder
	OpenSlotCountForExternalValidators int
}

// CompletedEPoSRound ..
type CompletedEPoSRound struct {
	MedianStake         numeric.Dec              `json:"epos-median-stake"`
	MaximumExternalSlot int                      `json:"max-external-slots"`
	AuctionWinners      []effective.SlotPurchase `json:"epos-slot-winners"`
	AuctionCandidates   []*CandidateOrder        `json:"epos-slot-candidates"`
}

// CandidateOrder ..
type CandidateOrder struct {
	*effective.SlotOrder
	StakePerKey *big.Int
	Validator   common.Address
}

// MarshalJSON ..
func (p CandidateOrder) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		*effective.SlotOrder
		StakePerKey *big.Int `json:"stake-per-key"`
		Validator   string   `json:"validator"`
	}{
		p.SlotOrder,
		p.StakePerKey,
		common2.MustAddressToBech32(p.Validator),
	})
}

// NewEPoSRound runs a fresh computation of EPoS using the latest data always.
func NewEPoSRound(epoch *big.Int, stakedReader StakingCandidatesReader, isExtendedBound bool, slotsLimit, shardCount int) (
	*CompletedEPoSRound, error,
) {
	eligibleCandidate, err := prepareOrders(stakedReader, slotsLimit, shardCount)
	if err != nil {
		return nil, err
	}
	maxExternalSlots := shard.ExternalSlotsAvailableForEpoch(
		epoch,
	)
	median, winners := effective.Apply(
		eligibleCandidate, maxExternalSlots, isExtendedBound,
	)
	auctionCandidates := make([]*CandidateOrder, 0, len(eligibleCandidate))

	for key := range eligibleCandidate {
		// NOTE in principle, a div-by-zero should not
		// happen by this point but the risk of not being explicit about
		// checking is a panic, so the check is worth it
		perKey := big.NewInt(0)
		if l := len(eligibleCandidate[key].SpreadAmong); l > 0 {
			perKey.Set(
				new(big.Int).Div(
					eligibleCandidate[key].Stake, big.NewInt(int64(l)),
				),
			)
		}
		auctionCandidates = append(auctionCandidates, &CandidateOrder{
			SlotOrder:   eligibleCandidate[key],
			StakePerKey: perKey,
			Validator:   key,
		})
	}

	return &CompletedEPoSRound{
		MedianStake:         median,
		MaximumExternalSlot: maxExternalSlots,
		AuctionWinners:      winners,
		AuctionCandidates:   auctionCandidates,
	}, nil
}

func prepareOrders(
	stakedReader StakingCandidatesReader,
	slotsLimit, shardCount int,
) (map[common.Address]*effective.SlotOrder, error) {
	candidates := stakedReader.ValidatorCandidates()
	blsKeys := map[bls.SerializedPublicKey]struct{}{}
	essentials := map[common.Address]*effective.SlotOrder{}
	totalStaked, tempZero := big.NewInt(0), numeric.ZeroDec()

	// Avoid duplicate BLS keys as harmony nodes
	instance := shard.Schedule.InstanceForEpoch(stakedReader.CurrentBlock().Epoch())
	for _, account := range instance.HmyAccounts() {
		pub := &bls_core.PublicKey{}
		if err := pub.DeserializeHexStr(account.BLSPublicKey); err != nil {
			continue
		}
		pubKey := bls.SerializedPublicKey{}
		if err := pubKey.FromLibBLSPublicKey(pub); err != nil {
			continue
		}
		blsKeys[pubKey] = struct{}{}
	}

	state, err := stakedReader.StateAt(stakedReader.CurrentBlock().Root())
	if err != nil || state == nil {
		return nil, errors.Wrapf(err, "not state found at root: %s", stakedReader.CurrentBlock().Root().Hex())
	}
	for i := range candidates {
		// TODO: reading validator wrapper from DB could be a bottle net when there are hundreds of validators
		// with thousands of delegator data.
		validator, err := stakedReader.ReadValidatorInformationAtState(
			candidates[i], state,
		)
		if err != nil {
			return nil, err
		}
		snapshot, err := stakedReader.ReadValidatorSnapshot(
			candidates[i],
		)
		if err != nil {
			return nil, err
		}
		if !IsEligibleForEPoSAuction(snapshot, validator) {
			continue
		}

		slotPubKeysLimited := make([]bls.SerializedPublicKey, 0, len(validator.SlotPubKeys))
		found := false
		shardSlotsCount := make([]int, shardCount)
		for _, key := range validator.SlotPubKeys {
			if _, ok := blsKeys[key]; ok {
				found = true
			} else {
				blsKeys[key] = struct{}{}
				shard := new(big.Int).Mod(key.Big(), big.NewInt(int64(shardCount))).Int64()
				if slotsLimit == 0 || shardSlotsCount[shard] < slotsLimit {
					slotPubKeysLimited = append(slotPubKeysLimited, key)
				}
				shardSlotsCount[shard]++
			}
		}

		if found {
			continue
		}

		validatorStake := big.NewInt(0)
		for i := range validator.Delegations {
			validatorStake.Add(
				validatorStake, validator.Delegations[i].Amount,
			)
		}

		totalStaked.Add(totalStaked, validatorStake)

		essentials[validator.Address] = &effective.SlotOrder{
			Stake:       validatorStake,
			SpreadAmong: slotPubKeysLimited,
			Percentage:  tempZero,
		}
	}
	totalStakedDec := numeric.NewDecFromBigInt(totalStaked)

	for _, value := range essentials {
		value.Percentage = numeric.NewDecFromBigInt(value.Stake).Quo(totalStakedDec)
	}

	return essentials, nil
}

// IsEligibleForEPoSAuction ..
func IsEligibleForEPoSAuction(snapshot *staking.ValidatorSnapshot, validator *staking.ValidatorWrapper) bool {
	// This original condition to check whether a validator is in last committee is not stable
	// because cross-links may arrive after the epoch ends and it still got counted into the
	// NumBlocksToSign, making this condition to be true when the validator is actually not in committee
	//if snapshot.Counters.NumBlocksToSign.Cmp(validator.Counters.NumBlocksToSign) != 0 {

	// Check whether the validator is in current committee
	if validator.LastEpochInCommittee.Cmp(snapshot.Epoch) == 0 {
		// validator was in last epoch's committee
		// validator with below-threshold signing activity won't be considered for next epoch
		// and their status will be turned to inactive in FinalizeNewBlock
		computed := availability.ComputeCurrentSigning(snapshot.Validator, validator)
		if computed.IsBelowThreshold {
			return false
		}
	}
	// For validators who were not in last epoch's committee
	// or for those who were and signed enough blocks,
	// the decision is based on the status
	switch validator.Status {
	case effective.Active:
		return true
	default:
		return false
	}
}

// ChainReader is a subset of Engine.Blockchain, just enough to do assignment
type ChainReader interface {
	// ReadShardState retrieves sharding state given the epoch number.
	// This api reads the shard state cached or saved on the chaindb.
	// Thus, only should be used to read the shard state of the current chain.
	ReadShardState(epoch *big.Int) (*shard.State, error)
	// GetHeader retrieves a block header from the database by hash and number.
	GetHeaderByHash(common.Hash) *block.Header
	// Config retrieves the blockchain's chain configuration.
	Config() *params.ChainConfig
	// CurrentHeader retrieves the current header from the local chain.
	CurrentHeader() *block.Header
}

// DataProvider ..
type DataProvider interface {
	StakingCandidatesReader
	ChainReader
}

type partialStakingEnabled struct{}

var (
	// WithStakingEnabled ..
	WithStakingEnabled Reader = partialStakingEnabled{}
	// ErrComputeForEpochInPast ..
	ErrComputeForEpochInPast = errors.New("cannot compute for epoch in past")
)

// This is the shard state computation logic before staking epoch.
func preStakingEnabledCommittee(s shardingconfig.Instance) (*shard.State, error) {
	shardNum := int(s.NumShards())
	shardHarmonyNodes := s.NumHarmonyOperatedNodesPerShard()
	shardSize := s.NumNodesPerShard()
	hmyAccounts := s.HmyAccounts()
	fnAccounts := s.FnAccounts()
	shardState := &shard.State{}
	// Shard state needs to be sorted by shard ID
	for i := 0; i < shardNum; i++ {
		com := shard.Committee{ShardID: uint32(i)}
		for j := 0; j < shardHarmonyNodes; j++ {
			index := i + j*shardNum // The initial account to use for genesis nodes
			pub := &bls_core.PublicKey{}
			pub.DeserializeHexStr(hmyAccounts[index].BLSPublicKey)
			pubKey := bls.SerializedPublicKey{}
			pubKey.FromLibBLSPublicKey(pub)
			// TODO: directly read address for bls too
			addr, err := common2.ParseAddr(hmyAccounts[index].Address)
			if err != nil {
				return nil, err
			}
			curNodeID := shard.Slot{
				EcdsaAddress: addr,
				BLSPublicKey: pubKey,
			}
			com.Slots = append(com.Slots, curNodeID)
		}
		// add FN runner's key
		for j := shardHarmonyNodes; j < shardSize; j++ {
			index := i + (j-shardHarmonyNodes)*shardNum
			pub := &bls_core.PublicKey{}
			pub.DeserializeHexStr(fnAccounts[index].BLSPublicKey)
			pubKey := bls.SerializedPublicKey{}
			pubKey.FromLibBLSPublicKey(pub)
			// TODO: directly read address for bls too
			addr, err := common2.ParseAddr(fnAccounts[index].Address)
			if err != nil {
				return nil, err
			}
			curNodeID := shard.Slot{
				EcdsaAddress: addr,
				BLSPublicKey: pubKey,
			}
			com.Slots = append(com.Slots, curNodeID)
		}
		shardState.Shards = append(shardState.Shards, com)
	}
	return shardState, nil
}

func eposStakedCommittee(
	epoch *big.Int, s shardingconfig.Instance, stakerReader DataProvider,
) (*shard.State, error) {
	shardCount := int(s.NumShards())
	//shardState := &shard.State{}
	shards := make([]shard.Committee, shardCount)
	hAccounts := s.HmyAccounts()
	shardHarmonyNodes := s.NumHarmonyOperatedNodesPerShard()

	for i := 0; i < shardCount; i++ {
		shards[i] = shard.Committee{ShardID: uint32(i), Slots: shard.SlotList{}}
		for j := 0; j < shardHarmonyNodes; j++ {
			index := i + j*shardCount
			pub := &bls_core.PublicKey{}
			if err := pub.DeserializeHexStr(hAccounts[index].BLSPublicKey); err != nil {
				return nil, err
			}
			pubKey := bls.SerializedPublicKey{}
			if err := pubKey.FromLibBLSPublicKey(pub); err != nil {
				return nil, err
			}

			addr, err := common2.ParseAddr(hAccounts[index].Address)
			if err != nil {
				return nil, err
			}
			// TODO: assign by random.
			shards[i].Slots = append(shards[i].Slots, shard.Slot{
				EcdsaAddress: addr,
				BLSPublicKey: pubKey,
			})
		}
	}

	// TODO(audit): make sure external validator BLS key are also not duplicate to Harmony's keys
	completedEPoSRound, err := NewEPoSRound(epoch, stakerReader, stakerReader.Config().IsEPoSBound35(epoch), s.SlotsLimit(), shardCount)
	if err != nil {
		return nil, err
	}

	shardBig := big.NewInt(int64(shardCount))
	for _, purchasedSlot := range completedEPoSRound.AuctionWinners {
		// TODO: assign by random.
		shardID := int(new(big.Int).Mod(purchasedSlot.Key.Big(), shardBig).Int64())
		shards[shardID].Slots = append(
			shards[shardID].Slots, shard.Slot{
				EcdsaAddress:   purchasedSlot.Addr,
				BLSPublicKey:   purchasedSlot.Key,
				EffectiveStake: &purchasedSlot.EPoSStake,
				RawStake:       purchasedSlot.RawStake,
			},
		)
	}

	// <Resharding>
	// 1) Detect which shards have lowest/highest stake.
	type shardRawStake struct {
		shardID  int
		rawStake numeric.Dec
	}
	shardsRawStake := make([]shardRawStake, 0, len(shards))
	for i, shard := range shards {
		total := numeric.ZeroDec()
		for _, slot := range shard.Slots {
			total = total.Add(slot.RawStake) // TODO: raw stake possible nil
		}
		shardsRawStake = append(shardsRawStake, shardRawStake{
			shardID:  i,
			rawStake: total,
		})
	}
	// Highest stake should be first.
	sort.SliceStable(shardsRawStake, func(i, j int) bool {
		if shardsRawStake[i].rawStake.Equal(shardsRawStake[j].rawStake) {
			return shardsRawStake[i].shardID > shardsRawStake[j].shardID
		}
		return shardsRawStake[i].rawStake.GT(shardsRawStake[j].rawStake)
	})

	highest, lowest := shardsRawStake[:len(shardsRawStake)/2], shardsRawStake[len(shardsRawStake)/2:]

	// TODO: use VRF as seed
	rnd := pseudorandom.NewXorshiftPseudorandom(uint32(epoch.Uint64()))

	// 2) Take 5% of slots from high stake shards and apply to low stake shards.
	for _, shard := range highest {
		fivePercentCount := int(float64(len(shards[shard.shardID].Slots)) * 0.05)
		// It's going to be difficult to test without this assignment.
		if fivePercentCount == 0 {
			if len(shards[shard.shardID].Slots) > 1 {
				fivePercentCount = 1
			} else {
				continue
			}
		}
		for i := 0; i < fivePercentCount; i++ {
			// Pick a random slot from the shard.
			n := int(rnd.Uint32n(uint32(len(shards[shard.shardID].Slots))))
			slot := shards[shard.shardID].Slots[n]
			// Remove the slot from the shard.
			shards[shard.shardID].Slots = append(
				shards[shard.shardID].Slots[:n],
				shards[shard.shardID].Slots[n+1:]...,
			)
			n = int(rnd.Uint32n(uint32(len(lowest))))
			// Add the slot to the lowest stake shard.
			shards[n].Slots = append(
				shards[n].Slots,
				slot,
			)
		}
	}
	// </Resharding>

	if len(completedEPoSRound.AuctionWinners) == 0 {
		instance := shard.Schedule.InstanceForEpoch(epoch)
		preInstance := shard.Schedule.InstanceForEpoch(new(big.Int).Sub(epoch, big.NewInt(1)))
		isTestnet := nodeconfig.GetDefaultConfig().GetNetworkType() == nodeconfig.Testnet
		isShardReduction := preInstance.NumShards() != instance.NumShards()
		// If the shard-reduction happens, we cannot use the old committee.
		if isTestnet && isShardReduction {
			utils.Logger().Warn().Msg("No elected validators in the new epoch!!! But use the new committee due to Testnet Shard Reduction.")
			return &shard.State{
				Epoch:  nil,
				Shards: shards,
			}, nil
		}
		utils.Logger().Warn().Msg("No elected validators in the new epoch!!! Reuse old shard state.")
		return stakerReader.ReadShardState(big.NewInt(0).Sub(epoch, big.NewInt(1)))
	}
	return &shard.State{
		Epoch:  nil,
		Shards: shards,
	}, nil
}

// ReadFromDB is a wrapper on ReadShardState
func (def partialStakingEnabled) ReadFromDB(
	epoch *big.Int, reader DataProvider,
) (newSuperComm *shard.State, err error) {
	return reader.ReadShardState(epoch)
}

// Compute is single entry point for
// computing a new super committee, aka new shard state
func (def partialStakingEnabled) Compute(
	epoch *big.Int, stakerReader DataProvider,
) (newSuperComm *shard.State, err error) {
	preStaking := true
	if stakerReader != nil {
		config := stakerReader.Config()
		if config.IsStaking(epoch) {
			preStaking = false
		}
	}

	instance := shard.Schedule.InstanceForEpoch(epoch)
	if preStaking {
		// Pre-staking shard state doesn't need to set epoch (backward compatible)
		return preStakingEnabledCommittee(instance)
	}
	// Sanity check, can't compute against epochs in past
	if e := stakerReader.CurrentHeader().Epoch(); epoch.Cmp(e) == -1 {
		utils.Logger().Error().Uint64("header-epoch", e.Uint64()).
			Uint64("compute-epoch", epoch.Uint64()).
			Msg("Tried to compute committee for epoch in past")
		return nil, ErrComputeForEpochInPast
	}
	utils.AnalysisStart("computeEPoSStakedCommittee")
	shardState, err := eposStakedCommittee(epoch, instance, stakerReader)
	utils.AnalysisEnd("computeEPoSStakedCommittee")

	if err != nil {
		return nil, err
	}

	// Set the epoch of shard state
	shardState.Epoch = big.NewInt(0).Set(epoch)
	utils.Logger().Info().
		Uint64("computed-for-epoch", epoch.Uint64()).
		Msg("computed new super committee")
	return shardState, nil
}
