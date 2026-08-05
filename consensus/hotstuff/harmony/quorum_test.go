package harmony

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/harmony-one/harmony/consensus/hotstuff"
	hmyquorum "github.com/harmony-one/harmony/consensus/quorum"
	"github.com/harmony-one/harmony/consensus/votepower"
	hmybls "github.com/harmony-one/harmony/crypto/bls"
	shardingconfig "github.com/harmony-one/harmony/internal/configs/sharding"
	"github.com/harmony-one/harmony/numeric"
	"github.com/harmony-one/harmony/shard"
	"github.com/stretchr/testify/require"
)

func TestHarmonyQuorumPreservesCanonicalBLSSlotOrderAndBitmap(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.ZeroDec())

	stake := numeric.OneDec()
	validatorA := common.HexToAddress("0x1")
	validatorB := common.HexToAddress("0x2")
	keyA1 := quorumTestKey()
	keyB := quorumTestKey()
	keyA2 := quorumTestKey()
	committee := &shard.Committee{
		ShardID: 1,
		Slots: shard.SlotList{
			{EcdsaAddress: validatorA, BLSPublicKey: keyA1, EffectiveStake: &stake},
			{EcdsaAddress: validatorB, BLSPublicKey: keyB, EffectiveStake: &stake},
			{EcdsaAddress: validatorA, BLSPublicKey: keyA2, EffectiveStake: &stake},
		},
	}

	quorum, err := NewHarmonyQuorum(committee, big.NewInt(1))
	require.NoError(t, err)

	slots := quorum.Slots()
	require.Equal(t, []hotstuff.MemberID{
		HarmonyQuorumMemberID(keyA1),
		HarmonyQuorumMemberID(keyB),
		HarmonyQuorumMemberID(keyA2),
	}, []hotstuff.MemberID{slots[0].ID, slots[1].ID, slots[2].ID})
	require.Equal(t, []common.Address{validatorA, validatorB, validatorA}, []common.Address{
		slots[0].Validator, slots[1].Validator, slots[2].Validator,
	})
	require.Equal(t, []hmybls.SerializedPublicKey{keyA1, keyB, keyA2}, []hmybls.SerializedPublicKey{
		slots[0].PublicKey, slots[1].PublicKey, slots[2].PublicKey,
	})

	bitmap, err := quorum.Bitmap([]hotstuff.MemberID{slots[2].ID, slots[0].ID})
	require.NoError(t, err)
	require.Equal(t, []byte{0b00000101}, bitmap)

	signers, err := quorum.Signers(bitmap)
	require.NoError(t, err)
	require.Equal(t, []hotstuff.MemberID{slots[0].ID, slots[2].ID}, signers)
}

func TestHarmonyQuorumUsesExactDecimalVotingPower(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.ZeroDec())

	stake := numeric.OneDec()
	committee := &shard.Committee{Slots: shard.SlotList{
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &stake},
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &stake},
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &stake},
	}}
	quorum, err := NewHarmonyQuorum(committee, big.NewInt(1))
	require.NoError(t, err)

	threshold := numeric.NewDec(2).Quo(numeric.NewDec(3))
	slots := quorum.Slots()
	require.True(t, slots[0].VotingPower.Equal(numeric.MustNewDecFromStr("0.333333333333333333")))
	require.True(t, slots[1].VotingPower.Equal(numeric.MustNewDecFromStr("0.333333333333333333")))
	require.True(t, slots[2].VotingPower.Equal(numeric.MustNewDecFromStr("0.333333333333333334")))
	require.True(t, quorum.Threshold().Equal(threshold))

	exactThreshold, err := quorum.Bitmap([]hotstuff.MemberID{slots[0].ID, slots[1].ID})
	require.NoError(t, err)
	power, err := quorum.VotingPower(exactThreshold)
	require.NoError(t, err)
	require.True(t, power.Equal(numeric.MustNewDecFromStr("0.666666666666666666")))
	formed, err := quorum.IsQuorum(exactThreshold)
	require.NoError(t, err)
	require.False(t, formed, "Harmony requires voting power strictly greater than two thirds")

	aboveThreshold, err := quorum.Bitmap([]hotstuff.MemberID{slots[0].ID, slots[2].ID})
	require.NoError(t, err)
	power, err = quorum.VotingPower(aboveThreshold)
	require.NoError(t, err)
	require.True(t, power.Equal(numeric.MustNewDecFromStr("0.666666666666666667")))
	formed, err = quorum.IsQuorum(aboveThreshold)
	require.NoError(t, err)
	require.False(t, formed, "power equal to Harmony's rounded threshold is not quorum")

	allSlots, err := quorum.Bitmap([]hotstuff.MemberID{slots[0].ID, slots[1].ID, slots[2].ID})
	require.NoError(t, err)
	formed, err = quorum.IsQuorum(allSlots)
	require.NoError(t, err)
	require.True(t, formed)

	oneAtomAboveStake := numeric.NewDecFromBigInt(big.NewInt(666666666666666668))
	remainderStake := numeric.NewDecFromBigInt(big.NewInt(333333333333333332))
	oneAtomAbove, err := NewHarmonyQuorum(&shard.Committee{Slots: shard.SlotList{
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &oneAtomAboveStake},
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &remainderStake},
	}}, big.NewInt(1))
	require.NoError(t, err)
	oneAtomAboveSlots := oneAtomAbove.Slots()
	oneSigner, err := oneAtomAbove.Bitmap([]hotstuff.MemberID{oneAtomAboveSlots[0].ID})
	require.NoError(t, err)
	power, err = oneAtomAbove.VotingPower(oneSigner)
	require.NoError(t, err)
	require.True(t, power.Equal(numeric.MustNewDecFromStr("0.666666666666666668")))
	formed, err = oneAtomAbove.IsQuorum(oneSigner)
	require.NoError(t, err)
	require.True(t, formed)
}

func TestHarmonyQuorumPreservesHarmonyAndEPoSSlotWeights(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.MustNewDecFromStr("0.4"))

	one := numeric.NewDec(1)
	two := numeric.NewDec(2)
	three := numeric.NewDec(3)
	committee := &shard.Committee{Slots: shard.SlotList{
		{BLSPublicKey: quorumTestKey(), EffectiveStake: nil},
		{BLSPublicKey: quorumTestKey(), EffectiveStake: nil},
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &one},
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &two},
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &three},
	}}
	quorum, err := NewHarmonyQuorum(committee, big.NewInt(1))
	require.NoError(t, err)

	slots := quorum.Slots()
	expected := []numeric.Dec{
		numeric.MustNewDecFromStr("0.2"),
		numeric.MustNewDecFromStr("0.2"),
		numeric.MustNewDecFromStr("0.1"),
		numeric.MustNewDecFromStr("0.2"),
		numeric.MustNewDecFromStr("0.3"),
	}
	for index := range expected {
		require.True(t, slots[index].VotingPower.Equal(expected[index]), "slot %d", index)
	}

	bitmap, err := quorum.Bitmap([]hotstuff.MemberID{slots[0].ID, slots[1].ID, slots[4].ID})
	require.NoError(t, err)
	formed, err := quorum.IsQuorum(bitmap)
	require.NoError(t, err)
	require.True(t, formed)

	externalOnly, err := quorum.Bitmap([]hotstuff.MemberID{slots[2].ID, slots[3].ID, slots[4].ID})
	require.NoError(t, err)
	formed, err = quorum.IsQuorum(externalOnly)
	require.NoError(t, err)
	require.False(t, formed, "three of five slots have only 0.6 EPoS voting power")
}

func TestHarmonyQuorumRejectsInvalidCommitteeInputs(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.ZeroDec())

	_, err := NewHarmonyQuorum(nil, big.NewInt(1))
	require.ErrorIs(t, err, shard.ErrSubCommitteeNil)
	_, err = NewHarmonyQuorum(&shard.Committee{Slots: shard.SlotList{{BLSPublicKey: quorumTestKey()}}}, nil)
	require.ErrorIs(t, err, ErrNilQuorumEpoch)
	_, err = NewHarmonyQuorum(&shard.Committee{}, big.NewInt(1))
	require.ErrorIs(t, err, hotstuff.ErrEmptyCommittee)

	key := quorumTestKey()
	stake := numeric.OneDec()
	_, err = NewHarmonyQuorum(&shard.Committee{Slots: shard.SlotList{
		{EcdsaAddress: common.HexToAddress("0x1"), BLSPublicKey: key, EffectiveStake: &stake},
		{EcdsaAddress: common.HexToAddress("0x1"), BLSPublicKey: key, EffectiveStake: &stake},
	}}, big.NewInt(1))
	require.ErrorIs(t, err, hotstuff.ErrDuplicateBLSPublicKey)
	_, err = NewHarmonyQuorum(&shard.Committee{Slots: shard.SlotList{
		{EcdsaAddress: common.HexToAddress("0x1"), BLSPublicKey: key, EffectiveStake: &stake},
		{EcdsaAddress: common.HexToAddress("0x2"), BLSPublicKey: key, EffectiveStake: &stake},
	}}, big.NewInt(1))
	require.ErrorIs(t, err, ErrDuplicateBLSKeyOwner)

	_, err = NewHarmonyQuorum(&shard.Committee{Slots: shard.SlotList{
		{BLSPublicKey: hmybls.SerializedPublicKey{}, EffectiveStake: &stake},
	}}, big.NewInt(1))
	require.ErrorIs(t, err, hotstuff.ErrInvalidBLSPublicKey)
	malformedKey := hmybls.SerializedPublicKey{}
	for index := range malformedKey {
		malformedKey[index] = 0xff
	}
	_, err = NewHarmonyQuorum(&shard.Committee{Slots: shard.SlotList{
		{BLSPublicKey: malformedKey, EffectiveStake: &stake},
	}}, big.NewInt(1))
	require.ErrorIs(t, err, hotstuff.ErrInvalidBLSPublicKey)

	zero := numeric.ZeroDec()
	_, err = NewHarmonyQuorum(&shard.Committee{Slots: shard.SlotList{
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &zero},
	}}, big.NewInt(1))
	require.ErrorIs(t, err, ErrInvalidQuorumVotingPower)
}

func TestHarmonyQuorumRejectsNonCanonicalBitmapAndSigners(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.ZeroDec())

	stake := numeric.OneDec()
	committee := &shard.Committee{Slots: make(shard.SlotList, 9)}
	for index := range committee.Slots {
		committee.Slots[index] = shard.Slot{BLSPublicKey: quorumTestKey(), EffectiveStake: &stake}
	}
	quorum, err := NewHarmonyQuorum(committee, big.NewInt(1))
	require.NoError(t, err)
	slots := quorum.Slots()

	_, err = quorum.Bitmap([]hotstuff.MemberID{slots[0].ID, slots[0].ID})
	require.ErrorIs(t, err, hotstuff.ErrDuplicateSigner)
	_, err = quorum.Bitmap([]hotstuff.MemberID{"unknown"})
	require.ErrorIs(t, err, hotstuff.ErrUnknownVoter)

	_, err = quorum.Signers([]byte{1})
	require.ErrorIs(t, err, ErrInvalidQuorumBitmap)
	_, err = quorum.Signers([]byte{0, 0b00000010})
	require.ErrorIs(t, err, ErrNonCanonicalQuorumBitmap)
	_, err = quorum.VotingPower([]byte{0, 0b10000000})
	require.ErrorIs(t, err, ErrNonCanonicalQuorumBitmap)
}

func TestHarmonyQuorumValidatesBitmapBoundaries(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.ZeroDec())

	for _, slotCount := range []int{1, 7, 8, 9} {
		t.Run(fmt.Sprintf("slots-%d", slotCount), func(t *testing.T) {
			stake := numeric.OneDec()
			committee := &shard.Committee{Slots: make(shard.SlotList, slotCount)}
			for index := range committee.Slots {
				committee.Slots[index] = shard.Slot{BLSPublicKey: quorumTestKey(), EffectiveStake: &stake}
			}
			bridge, err := NewHarmonyQuorum(committee, big.NewInt(1))
			require.NoError(t, err)

			bitmapLength := (slotCount + 7) >> 3
			canonical := make([]byte, bitmapLength)
			if slotCount&7 == 0 {
				canonical[bitmapLength-1] = 0xff
			} else {
				canonical[bitmapLength-1] = byte((1 << uint(slotCount&7)) - 1)
			}
			_, err = bridge.Signers(canonical)
			require.NoError(t, err)

			for bit := slotCount; bit < bitmapLength*8; bit++ {
				nonCanonical := append([]byte(nil), canonical...)
				nonCanonical[bit>>3] |= byte(1) << uint(bit&7)
				_, err = bridge.Signers(nonCanonical)
				require.ErrorIs(t, err, ErrNonCanonicalQuorumBitmap, "bit %d", bit)
			}
		})
	}
}

func TestHarmonyQuorumOwnsReturnedVotingPower(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.ZeroDec())

	stake := numeric.OneDec()
	quorum, err := NewHarmonyQuorum(&shard.Committee{Slots: shard.SlotList{
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &stake},
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &stake},
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &stake},
	}}, big.NewInt(1))
	require.NoError(t, err)

	first := quorum.Slots()
	first[0].VotingPower.Int.SetInt64(0)
	second := quorum.Slots()
	require.True(t, second[0].VotingPower.IsPositive())

	threshold := quorum.Threshold()
	threshold.Int.SetInt64(0)
	require.True(t, quorum.Threshold().IsPositive())
}

func TestHarmonyQuorumMatchesHarmonyRosterAndVerifier(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.MustNewDecFromStr("0.4"))

	one := numeric.NewDec(1)
	two := numeric.NewDec(2)
	three := numeric.NewDec(3)
	committee := &shard.Committee{Slots: shard.SlotList{
		{BLSPublicKey: quorumTestKey()},
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &one},
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &two},
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &three},
	}}
	epoch := big.NewInt(1)
	bridge, err := NewHarmonyQuorum(committee, epoch)
	require.NoError(t, err)
	roster, err := votepower.Compute(committee, epoch)
	require.NoError(t, err)
	verifier, err := hmyquorum.NewVerifier(committee, epoch, true)
	require.NoError(t, err)

	publics := make([]hmybls.PublicKeyWrapper, len(committee.Slots))
	for index, slot := range committee.Slots {
		public, err := hmybls.BytesToBLSPublicKey(slot.BLSPublicKey.Bytes())
		require.NoError(t, err)
		publics[index] = hmybls.PublicKeyWrapper{Bytes: slot.BLSPublicKey, Object: public}
	}
	for raw := byte(0); raw < 1<<len(committee.Slots); raw++ {
		bitmap := []byte{raw}
		mask := hmybls.NewMask(publics)
		require.NoError(t, mask.SetMask(bitmap))

		power, err := bridge.VotingPower(bitmap)
		require.NoError(t, err)
		require.True(t, power.Equal(roster.VotePowerByMask(mask)), "bitmap %08b", raw)
		formed, err := bridge.IsQuorum(bitmap)
		require.NoError(t, err)
		require.Equal(t, verifier.IsQuorumAchievedByMask(mask), formed, "bitmap %08b", raw)
	}
}

func TestHarmonyQuorumOwnsCommitteeInputs(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.ZeroDec())

	one := numeric.OneDec()
	key := quorumTestKey()
	committee := &shard.Committee{Slots: shard.SlotList{
		{BLSPublicKey: key, EffectiveStake: &one},
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &one},
		{BLSPublicKey: quorumTestKey(), EffectiveStake: &one},
	}}
	bridge, err := NewHarmonyQuorum(committee, big.NewInt(1))
	require.NoError(t, err)

	committee.Slots[0].BLSPublicKey = quorumTestKey()
	committee.Slots[0].EffectiveStake.Int.SetInt64(0)
	slots := bridge.Slots()
	require.Equal(t, key, slots[0].PublicKey)
	require.True(t, slots[0].VotingPower.IsPositive())
}

func TestHarmonyQuorumRejectsMalformedDecimalAndEpoch(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.ZeroDec())

	key := quorumTestKey()
	malformed := numeric.Dec{}
	_, err := NewHarmonyQuorum(&shard.Committee{Slots: shard.SlotList{
		{BLSPublicKey: key, EffectiveStake: &malformed},
	}}, big.NewInt(1))
	require.ErrorIs(t, err, ErrInvalidQuorumVotingPower)

	negative := numeric.NewDec(-1)
	_, err = NewHarmonyQuorum(&shard.Committee{Slots: shard.SlotList{
		{BLSPublicKey: key, EffectiveStake: &negative},
	}}, big.NewInt(1))
	require.ErrorIs(t, err, ErrInvalidQuorumVotingPower)

	positive := numeric.OneDec()
	_, err = NewHarmonyQuorum(&shard.Committee{Slots: shard.SlotList{
		{BLSPublicKey: key, EffectiveStake: &positive},
	}}, big.NewInt(-1))
	require.ErrorIs(t, err, ErrInvalidQuorumEpoch)
}

func useHarmonyQuorumSchedule(t *testing.T, harmonyPercent numeric.Dec) {
	t.Helper()
	instance, err := shardingconfig.NewInstance(
		1, 16, 1, 0, harmonyPercent,
		nil, nil, shardingconfig.Allowlist{}, nil,
		numeric.ZeroDec(), common.Address{}, nil, 100,
	)
	require.NoError(t, err)
	previous := shard.Schedule
	shard.Schedule = shardingconfig.NewFixedSchedule(instance)
	t.Cleanup(func() { shard.Schedule = previous })
}

func quorumTestKey() hmybls.SerializedPublicKey {
	return hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey()).Pub.Bytes
}
