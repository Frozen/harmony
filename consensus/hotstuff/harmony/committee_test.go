package harmony

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/harmony-one/harmony/consensus/hotstuff"
	hmybls "github.com/harmony-one/harmony/crypto/bls"
	"github.com/harmony-one/harmony/shard"
	"github.com/stretchr/testify/require"
)

func TestValidatorScheduleRotatesByValidatorAcrossMultipleBLSKeys(t *testing.T) {
	validatorA := common.HexToAddress("0x1")
	validatorB := common.HexToAddress("0x2")
	validatorC := common.HexToAddress("0x3")
	keyA1 := serializedKey(1)
	keyA2 := serializedKey(2)
	keyB := serializedKey(3)
	keyC := serializedKey(4)

	schedule, err := NewValidatorSchedule(&shard.Committee{
		ShardID: 0,
		Slots: shard.SlotList{
			{EcdsaAddress: validatorA, BLSPublicKey: keyA1},
			{EcdsaAddress: validatorA, BLSPublicKey: keyA2},
			{EcdsaAddress: validatorB, BLSPublicKey: keyB},
			{EcdsaAddress: validatorC, BLSPublicKey: keyC},
		},
	})
	require.NoError(t, err)

	validatorAID := hotstuff.MemberID(validatorA.Hex())
	validatorBID := hotstuff.MemberID(validatorB.Hex())
	validatorCID := hotstuff.MemberID(validatorC.Hex())
	require.Equal(t, []hotstuff.MemberID{validatorAID, validatorBID, validatorCID}, schedule.Members())
	require.Equal(t, validatorAID, schedule.Leader(1))
	require.Equal(t, validatorBID, schedule.Leader(2))
	require.Equal(t, validatorCID, schedule.Leader(3))
	require.Equal(t, validatorAID, schedule.Leader(4))

	owner, found := schedule.ValidatorForKey(keyA2)
	require.True(t, found)
	require.Equal(t, validatorAID, owner)
}

func TestValidatorScheduleRejectsBLSKeyOwnedByDifferentValidators(t *testing.T) {
	key := serializedKey(1)
	_, err := NewValidatorSchedule(&shard.Committee{
		ShardID: 0,
		Slots: shard.SlotList{
			{EcdsaAddress: common.HexToAddress("0x1"), BLSPublicKey: key},
			{EcdsaAddress: common.HexToAddress("0x2"), BLSPublicKey: key},
		},
	})
	require.ErrorIs(t, err, ErrDuplicateBLSKeyOwner)
}

func TestValidatorScheduleRejectsDuplicateBLSKeyForOneValidator(t *testing.T) {
	validator := common.HexToAddress("0x1")
	key := serializedKey(1)
	_, err := NewValidatorSchedule(&shard.Committee{
		ShardID: 0,
		Slots: shard.SlotList{
			{EcdsaAddress: validator, BLSPublicKey: key},
			{EcdsaAddress: validator, BLSPublicKey: key},
		},
	})
	require.ErrorIs(t, err, hotstuff.ErrDuplicateBLSPublicKey)
}

func serializedKey(marker byte) hmybls.SerializedPublicKey {
	var key hmybls.SerializedPublicKey
	key[0] = marker
	return key
}
