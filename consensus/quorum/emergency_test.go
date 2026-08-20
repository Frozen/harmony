package quorum

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	bls_core "github.com/harmony-one/bls/ffi/go/bls"
	"github.com/harmony-one/harmony/consensus/votepower"
	"github.com/harmony-one/harmony/crypto/bls"
	shardingconfig "github.com/harmony-one/harmony/internal/configs/sharding"
	"github.com/harmony-one/harmony/internal/params"
	"github.com/harmony-one/harmony/numeric"
	"github.com/harmony-one/harmony/shard"
	"github.com/stretchr/testify/require"
)

func TestEmergencyVotingPowerActivationBoundary(t *testing.T) {
	require.Equal(t, emergencyShard0Block, shardingconfig.MainnetSchedule.EpochLastBlock(emergencyShard0Epoch))

	hasher := sha256.New()
	for _, key := range emergencyShard0ExcludedKeyManifest {
		_, err := hasher.Write(key[:])
		require.NoError(t, err)
	}
	require.Equal(t, "c287ca243feb79036ddc55fcefa6fcc8de3e97b4b9d9aeeeb2750d3cfc12f799", hex.EncodeToString(hasher.Sum(nil)))
	require.Len(t, emergencyShard0ExcludedKeys, 41)
	for _, key := range emergencyShard0ExcludedKeyManifest {
		_, err := bls.BytesToBLSPublicKey(key[:])
		require.NoError(t, err, key.Hex())
	}

	activeRoster := votepower.NewRoster(shard.BeaconChainShardID)
	activeEpoch := new(big.Int).SetUint64(emergencyShard0Epoch)
	activeContext := VotingPowerContext{
		ChainID:     new(big.Int).Set(params.MainnetChainID),
		BlockNumber: emergencyShard0Block,
		ParentHash:  emergencyShard0ParentHash,
	}

	require.True(t, emergencyVotingPowerApplies(activeRoster, activeEpoch, activeContext))
	require.True(t, RequiresVotingPowerRefreshAfterBlock(
		params.MainnetChainID, shard.BeaconChainShardID,
		emergencyShard0Block-1, emergencyShard0ParentHash,
	))
	require.False(t, RequiresVotingPowerRefreshAfterBlock(
		params.MainnetChainID, shard.BeaconChainShardID,
		emergencyShard0Block-2, emergencyShard0ParentHash,
	))
	require.False(t, RequiresVotingPowerRefreshAfterBlock(
		big.NewInt(2), shard.BeaconChainShardID,
		emergencyShard0Block-1, emergencyShard0ParentHash,
	))
	require.False(t, RequiresVotingPowerRefreshAfterBlock(
		params.MainnetChainID, 1,
		emergencyShard0Block-1, emergencyShard0ParentHash,
	))
	require.False(t, RequiresVotingPowerRefreshAfterBlock(
		params.MainnetChainID, shard.BeaconChainShardID,
		emergencyShard0Block-1, common.HexToHash("0x01"),
	))

	wrongShard := votepower.NewRoster(1)
	wrongEpoch := new(big.Int).SetUint64(emergencyShard0Epoch - 1)
	wrongChain := activeContext
	wrongChain.ChainID = big.NewInt(2)
	previousBlock := activeContext
	previousBlock.BlockNumber--
	nextBlock := activeContext
	nextBlock.BlockNumber++
	wrongParent := activeContext
	wrongParent.ParentHash = common.HexToHash("0x01")

	tests := []struct {
		name    string
		roster  *votepower.Roster
		epoch   *big.Int
		ctx     VotingPowerContext
		wantErr bool
	}{
		{name: "wrong shard", roster: wrongShard, epoch: activeEpoch, ctx: activeContext},
		{name: "wrong epoch", roster: activeRoster, epoch: wrongEpoch, ctx: activeContext, wantErr: true},
		{name: "wrong chain", roster: activeRoster, epoch: activeEpoch, ctx: wrongChain},
		{name: "previous block", roster: activeRoster, epoch: activeEpoch, ctx: previousBlock},
		{name: "next block", roster: activeRoster, epoch: activeEpoch, ctx: nextBlock},
		{name: "wrong parent", roster: activeRoster, epoch: activeEpoch, ctx: wrongParent, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.False(t, emergencyVotingPowerApplies(test.roster, test.epoch, test.ctx))
			got, err := effectiveRosterForContext(test.roster, test.epoch, test.ctx)
			if test.wantErr {
				require.Error(t, err)
				if test.name == "wrong epoch" {
					require.ErrorContains(t, err, "unexpected emergency epoch")
				} else {
					require.ErrorContains(t, err, "unexpected emergency parent hash")
				}
				return
			}
			require.NoError(t, err)
			require.Same(t, test.roster, got)
		})
	}
}

func TestNormalizeVotingPowerExcludingKeys(t *testing.T) {
	key0 := bls.SerializedPublicKey{0}
	key1 := bls.SerializedPublicKey{1}
	key2 := bls.SerializedPublicKey{2}
	roster := votepower.NewRoster(shard.BeaconChainShardID)
	roster.OrderedSlots = []bls.SerializedPublicKey{key0, key1, key2}
	roster.Voters[key0] = testVote(key0, "0.2", false)
	roster.Voters[key1] = testVote(key1, "0.3", true)
	roster.Voters[key2] = testVote(key2, "0.5", false)
	roster.OurVotingPowerTotalPercentage = numeric.MustNewDecFromStr("0.3")
	roster.TheirVotingPowerTotalPercentage = numeric.MustNewDecFromStr("0.7")

	got, err := normalizeVotingPowerExcludingKeys(roster, map[bls.SerializedPublicKey]struct{}{key0: {}})
	require.NoError(t, err)
	require.True(t, got.Voters[key0].OverallPercent.IsZero())
	require.True(t, got.Voters[key1].OverallPercent.Equal(numeric.MustNewDecFromStr("0.375")))
	require.True(t, got.Voters[key2].OverallPercent.Equal(numeric.MustNewDecFromStr("0.625")))
	require.True(t, got.OurVotingPowerTotalPercentage.Equal(numeric.MustNewDecFromStr("0.375")))
	require.True(t, got.TheirVotingPowerTotalPercentage.Equal(numeric.MustNewDecFromStr("0.625")))
	require.Equal(t, roster.OrderedSlots, got.OrderedSlots)
	require.NotSame(t, roster.Voters[key1].OverallPercent.Int, got.Voters[key1].OverallPercent.Int)

	// The canonical committee roster and its bitmap positions remain untouched.
	require.True(t, roster.Voters[key0].OverallPercent.Equal(numeric.MustNewDecFromStr("0.2")))
	require.True(t, roster.Voters[key1].OverallPercent.Equal(numeric.MustNewDecFromStr("0.3")))
	require.True(t, roster.Voters[key2].OverallPercent.Equal(numeric.MustNewDecFromStr("0.5")))
}

func TestNormalizeVotingPowerResidue(t *testing.T) {
	tests := []struct {
		name     string
		powers   []string
		expected []string
	}{
		{
			name:     "positive one atom correction",
			powers:   []string{"0.1", "0.2", "0.3", "0.4"},
			expected: []string{"0", "0.222222222222222222", "0.333333333333333333", "0.444444444444444445"},
		},
		{
			name:     "negative two atom correction",
			powers:   []string{"0.1", "0.15", "0.15", "0.15", "0.15", "0.15", "0.15"},
			expected: []string{"0", "0.166666666666666667", "0.166666666666666667", "0.166666666666666667", "0.166666666666666667", "0.166666666666666667", "0.166666666666666665"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			roster := testRoster(test.powers...)
			got, err := normalizeVotingPowerExcludingKeys(roster, map[bls.SerializedPublicKey]struct{}{roster.OrderedSlots[0]: {}})
			require.NoError(t, err)
			for i, key := range got.OrderedSlots {
				require.True(t, got.Voters[key].OverallPercent.Equal(numeric.MustNewDecFromStr(test.expected[i])), "slot %d: %s", i, got.Voters[key].OverallPercent.String())
			}
		})
	}
}

func TestNormalizeVotingPowerRejectsMalformedRoster(t *testing.T) {
	key0 := bls.SerializedPublicKey{0}
	key1 := bls.SerializedPublicKey{1}
	tests := []struct {
		name     string
		roster   *votepower.Roster
		excluded map[bls.SerializedPublicKey]struct{}
		want     string
	}{
		{name: "nil", roster: nil, excluded: map[bls.SerializedPublicKey]struct{}{}, want: "nil voting-power roster"},
		{name: "non unit total", roster: testRoster("0.4", "0.5"), excluded: map[bls.SerializedPublicKey]struct{}{key0: {}}, want: "canonical voting power total"},
		{name: "negative power", roster: testRoster("-0.1", "1.1"), excluded: map[bls.SerializedPublicKey]struct{}{key0: {}}, want: "negative voting power"},
		{name: "all power excluded", roster: testRoster("1"), excluded: map[bls.SerializedPublicKey]struct{}{key0: {}}, want: "non-positive remaining voting power"},
		{
			name: "duplicate ordered key",
			roster: &votepower.Roster{
				ShardID:      shard.BeaconChainShardID,
				OrderedSlots: []bls.SerializedPublicKey{key0, key0},
				Voters: map[bls.SerializedPublicKey]*votepower.AccommodateHarmonyVote{
					key0: testVote(key0, "1", false),
				},
			},
			excluded: map[bls.SerializedPublicKey]struct{}{key1: {}},
			want:     "duplicate ordered BLS key",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeVotingPowerExcludingKeys(test.roster, test.excluded)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestStakedDeciderUsesEmergencyVotingPowerContext(t *testing.T) {
	previousSchedule := shard.Schedule
	shard.Schedule = shardingconfig.MainnetSchedule
	t.Cleanup(func() { shard.Schedule = previousSchedule })

	committee := &shard.Committee{ShardID: shard.BeaconChainShardID}
	for _, key := range emergencyShard0ExcludedKeyManifest {
		stake := numeric.NewDec(1)
		committee.Slots = append(committee.Slots, shard.Slot{
			BLSPublicKey:   key,
			EffectiveStake: &stake,
		})
	}
	for scalar := byte(2); scalar < byte(157); scalar++ {
		key, _ := testBLSKey(t, scalar)
		zero := numeric.ZeroDec()
		committee.Slots = append(committee.Slots, shard.Slot{
			BLSPublicKey:   key,
			EffectiveStake: &zero,
		})
	}
	survivorSecret := bls_core.SecretKey{}
	survivorScalar := make([]byte, 32)
	survivorScalar[31] = 1
	require.NoError(t, survivorSecret.Deserialize(survivorScalar))
	survivorWrapper := bls.PublicKeyWrapper{Object: survivorSecret.GetPublicKey()}
	survivorWrapper.Bytes.FromLibBLSPublicKey(survivorWrapper.Object)
	survivor := survivorWrapper.Bytes
	survivorStake := numeric.NewDec(59)
	committee.Slots = append(committee.Slots, shard.Slot{
		BLSPublicKey:   survivor,
		EffectiveStake: &survivorStake,
	})
	require.Len(t, committee.Slots, emergencyShard0CommitteeSize)

	canonicalDecider := NewDecider(SuperMajorityStake, shard.BeaconChainShardID)
	_, err := canonicalDecider.SetVoters(
		committee,
		new(big.Int).SetUint64(emergencyShard0Epoch),
		true,
	)
	require.NoError(t, err)
	require.True(t, canonicalDecider.(*stakedVoteWeight).roster.Voters[emergencyShard0ExcludedKeyManifest[0]].OverallPercent.IsPositive())

	decider := NewDecider(SuperMajorityStake, shard.BeaconChainShardID)
	_, err = decider.SetVotersWithContext(
		committee,
		new(big.Int).SetUint64(emergencyShard0Epoch),
		true,
		VotingPowerContext{
			ChainID:     new(big.Int).Set(params.MainnetChainID),
			BlockNumber: emergencyShard0Block,
			ParentHash:  emergencyShard0ParentHash,
		},
	)
	require.NoError(t, err)

	weighted := decider.(*stakedVoteWeight)
	for _, key := range emergencyShard0ExcludedKeyManifest {
		require.True(t, weighted.roster.Voters[key].OverallPercent.IsZero())
	}
	require.True(t, weighted.roster.Voters[survivor].OverallPercent.Equal(numeric.OneDec()))
	require.Len(t, weighted.roster.OrderedSlots, len(committee.Slots))
	require.True(t, committee.Slots[0].EffectiveStake.Equal(numeric.OneDec()))

	mask := &bls.Mask{
		Bitmap:       make([]byte, (len(committee.Slots)+7)/8),
		Publics:      make([]*bls.PublicKeyWrapper, len(committee.Slots)),
		PublicsIndex: make(map[bls.SerializedPublicKey]int, len(committee.Slots)),
	}
	for i, slot := range committee.Slots {
		mask.PublicsIndex[slot.BLSPublicKey] = i
	}
	survivorIndex := len(committee.Slots) - 1
	mask.Bitmap[survivorIndex>>3] |= byte(1) << uint(survivorIndex&7)
	require.True(t, decider.IsQuorumAchievedByMask(mask))

	verifier, err := NewVerifier(committee, new(big.Int).SetUint64(emergencyShard0Epoch), true, true)
	require.NoError(t, err)
	require.False(t, verifier.IsQuorumAchievedByMask(mask, VotingPowerContext{}))
	require.True(t, verifier.IsQuorumAchievedByMask(mask, VotingPowerContext{
		ChainID:     new(big.Int).Set(params.MainnetChainID),
		BlockNumber: emergencyShard0Block,
		ParentHash:  emergencyShard0ParentHash,
	}))

	excluded := emergencyShard0ExcludedKeyManifest[0]
	excludedObject, err := bls.BytesToBLSPublicKey(excluded[:])
	require.NoError(t, err)
	excludedWrapper := bls.PublicKeyWrapper{Object: excludedObject, Bytes: excluded}
	signature := survivorSecret.SignHash(common.Hash{}.Bytes())
	for _, phase := range []Phase{Prepare, Commit, ViewChange} {
		decider.ResetPrepareAndCommitVotes()
		decider.ResetViewChangeVotes()
		_, err = decider.AddNewVote(phase, []*bls.PublicKeyWrapper{&excludedWrapper}, signature, common.Hash{}, emergencyShard0Block, 1)
		require.NoError(t, err)
		power := testCurrentTally(weighted, phase)
		require.True(t, power.IsZero(), phase.String())
		if phase == Commit {
			require.False(t, decider.IsAllSigsCollected())
		}

		_, err = decider.AddNewVote(phase, []*bls.PublicKeyWrapper{&survivorWrapper}, signature, common.Hash{}, emergencyShard0Block, 1)
		require.NoError(t, err)
		power = testCurrentTally(weighted, phase)
		require.True(t, power.Equal(numeric.OneDec()), "%s: %s", phase.String(), power.String())
		if phase == Commit {
			require.True(t, decider.IsAllSigsCollected())
		}
	}
}

func TestStakeVerifierUsesSignedBlockContext(t *testing.T) {
	roster := votepower.NewRoster(shard.BeaconChainShardID)
	mask := &bls.Mask{
		Bitmap:       make([]byte, (emergencyShard0CommitteeSize+7)/8),
		Publics:      make([]*bls.PublicKeyWrapper, 0, emergencyShard0CommitteeSize),
		PublicsIndex: make(map[bls.SerializedPublicKey]int, emergencyShard0CommitteeSize),
	}

	index := 0
	for _, key := range emergencyShard0ExcludedKeyManifest {
		roster.OrderedSlots = append(roster.OrderedSlots, key)
		roster.Voters[key] = testVote(key, "0.01", false)
		mask.Publics = append(mask.Publics, nil)
		mask.PublicsIndex[key] = index
		index++
	}
	for scalar := byte(2); scalar < byte(157); scalar++ {
		key, _ := testBLSKey(t, scalar)
		roster.OrderedSlots = append(roster.OrderedSlots, key)
		roster.Voters[key] = testVote(key, "0", false)
		mask.Publics = append(mask.Publics, nil)
		mask.PublicsIndex[key] = index
		index++
	}
	survivor := bls.SerializedPublicKey{0xff}
	roster.OrderedSlots = append(roster.OrderedSlots, survivor)
	roster.Voters[survivor] = testVote(survivor, "0.59", false)
	mask.Publics = append(mask.Publics, nil)
	mask.PublicsIndex[survivor] = index
	mask.Bitmap[index>>3] |= byte(1) << uint(index&7)
	require.Len(t, roster.OrderedSlots, emergencyShard0CommitteeSize)

	verifier := &stakeVerifier{
		r:     *roster,
		epoch: new(big.Int).SetUint64(emergencyShard0Epoch),
	}
	require.False(t, verifier.IsQuorumAchievedByMask(mask, VotingPowerContext{}))
	require.True(t, verifier.IsQuorumAchievedByMask(mask, VotingPowerContext{
		ChainID:     new(big.Int).Set(params.MainnetChainID),
		BlockNumber: emergencyShard0Block,
		ParentHash:  emergencyShard0ParentHash,
	}))
}

func TestEmergencyVotingPowerRequiresEveryExcludedKey(t *testing.T) {
	roster := testRoster("1")
	ctx := VotingPowerContext{
		ChainID:     new(big.Int).Set(params.MainnetChainID),
		BlockNumber: emergencyShard0Block,
		ParentHash:  emergencyShard0ParentHash,
	}

	_, err := effectiveRosterForContext(roster, new(big.Int).SetUint64(emergencyShard0Epoch), ctx)
	require.ErrorContains(t, err, "missing emergency BLS key")
}

func testBLSKey(t *testing.T, scalar byte) (bls.SerializedPublicKey, bls_core.SecretKey) {
	t.Helper()
	secret := bls_core.SecretKey{}
	encoded := make([]byte, 32)
	encoded[0] = scalar
	require.NoError(t, secret.Deserialize(encoded))
	key := bls.SerializedPublicKey{}
	key.FromLibBLSPublicKey(secret.GetPublicKey())
	return key, secret
}

func testCurrentTally(weighted *stakedVoteWeight, phase Phase) numeric.Dec {
	switch phase {
	case Prepare:
		return weighted.voteTally.Prepare.tally
	case Commit:
		return weighted.voteTally.Commit.tally
	case ViewChange:
		return weighted.voteTally.ViewChange.tally
	default:
		panic("unknown phase")
	}
}

func testVote(key bls.SerializedPublicKey, power string, harmony bool) *votepower.AccommodateHarmonyVote {
	return &votepower.AccommodateHarmonyVote{
		PureStakedVote: votepower.PureStakedVote{Identity: key},
		OverallPercent: numeric.MustNewDecFromStr(power),
		IsHarmonyNode:  harmony,
	}
}

func testRoster(powers ...string) *votepower.Roster {
	roster := votepower.NewRoster(shard.BeaconChainShardID)
	for i, power := range powers {
		key := bls.SerializedPublicKey{byte(i)}
		roster.OrderedSlots = append(roster.OrderedSlots, key)
		roster.Voters[key] = testVote(key, power, false)
	}
	return roster
}
