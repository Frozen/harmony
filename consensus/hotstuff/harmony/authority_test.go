package harmony

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/harmony-one/harmony/consensus/hotstuff"
	hmybls "github.com/harmony-one/harmony/crypto/bls"
	bls_core "github.com/harmony-one/harmony/crypto/bls/core"
	"github.com/harmony-one/harmony/numeric"
	"github.com/harmony-one/harmony/shard"
	"github.com/stretchr/testify/require"
)

func TestStakingQCAuthorityUsesExactHarmonyQuorumForQCFormationAndVerification(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.MustNewDecFromStr("0.4"))
	keys, secrets := authorityTestKeys(t, 4)
	stakeOne := numeric.NewDec(1)
	stakeTwo := numeric.NewDec(2)
	stakeThree := numeric.NewDec(3)
	source := &shard.Committee{ShardID: 2, Slots: shard.SlotList{
		{EcdsaAddress: common.HexToAddress("0x01"), BLSPublicKey: keys[0]},
		{EcdsaAddress: common.HexToAddress("0x02"), BLSPublicKey: keys[1], EffectiveStake: &stakeOne},
		{EcdsaAddress: common.HexToAddress("0x03"), BLSPublicKey: keys[2], EffectiveStake: &stakeTwo},
		{EcdsaAddress: common.HexToAddress("0x04"), BLSPublicKey: keys[3], EffectiveStake: &stakeThree},
	}}
	domain := hotstuff.VoteDomain{ChainID: 7, ShardID: 2, Epoch: 1, Genesis: "genesis"}
	authority, err := NewStakingQCAuthority(source, big.NewInt(1), domain)
	require.NoError(t, err)
	ids := authorityTestMemberIDs(keys)

	lowStake := authority.NewVoteSet("low-stake", 1)
	for _, index := range []int{1, 2, 3} {
		addAuthorityTestVote(t, lowStake, domain, ids[index], "low-stake", 1, secrets[index])
	}
	_, formed, err := lowStake.QC()
	require.NoError(t, err)
	require.False(t, formed, "three of four slots have only 0.6 EPoS power")

	highStake := authority.NewVoteSet("high-stake", 1)
	for _, index := range []int{0, 3} {
		addAuthorityTestVote(t, highStake, domain, ids[index], "high-stake", 1, secrets[index])
	}
	qc, formed, err := highStake.QC()
	require.NoError(t, err)
	require.True(t, formed, "two of four slots have 0.7 EPoS power")
	require.Equal(t, []hotstuff.MemberID{ids[0], ids[3]}, qc.QC.Signers)
	require.Equal(t, []byte{0b00001001}, qc.Bitmap)
	verifiedQC, err := authority.Verify(qc)
	require.NoError(t, err)
	_, _, err = authority.NewCore(hotstuff.Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	pacemaker, err := authority.NewPacemaker(1)
	require.NoError(t, err)
	require.NoError(t, authority.Advance(pacemaker, verifiedQC))
	require.Equal(t, hotstuff.View(2), pacemaker.CurrentView())

	timeouts := hotstuff.NewBLSTimeoutSet(authority, 2)
	for _, index := range []int{0, 3} {
		addAuthorityTestTimeout(
			t, timeouts, domain, ids[index], 2, qc.QC,
			qc, secrets[index],
		)
	}
	timeoutCertificate, formed, err := timeouts.Certificate()
	require.NoError(t, err)
	require.True(t, formed)
	verifiedTC, err := authority.VerifyTC(timeoutCertificate)
	require.NoError(t, err)
	require.NoError(t, authority.AdvanceTimeout(pacemaker, verifiedTC))
	require.Equal(t, hotstuff.View(3), pacemaker.CurrentView())
	require.Equal(t, qc.QC, pacemaker.HighQC())
	badBitmap := qc
	badBitmap.Bitmap = append([]byte(nil), qc.Bitmap...)
	badBitmap.Bitmap[0] |= 1 << 7
	_, err = authority.Verify(badBitmap)
	require.ErrorIs(t, err, hotstuff.ErrInvalidQCBitmap)

	tampered := qc
	tampered.QC.Signers = append([]hotstuff.MemberID(nil), ids[1], ids[2], ids[3])
	tampered.Bitmap = []byte{0b00001110}
	_, err = authority.Verify(tampered)
	require.ErrorIs(t, err, hotstuff.ErrInsufficientVotingPower)
}

func TestStakingQCAuthorityAcceptsOneDecimalAtomAboveThreshold(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.ZeroDec())
	keys, secrets := authorityTestKeys(t, 2)
	above := numeric.NewDecFromBigInt(big.NewInt(666666666666666668))
	below := numeric.NewDecFromBigInt(big.NewInt(333333333333333332))
	source := &shard.Committee{ShardID: 1, Slots: shard.SlotList{
		{EcdsaAddress: common.HexToAddress("0x11"), BLSPublicKey: keys[0], EffectiveStake: &above},
		{EcdsaAddress: common.HexToAddress("0x12"), BLSPublicKey: keys[1], EffectiveStake: &below},
	}}
	domain := hotstuff.VoteDomain{ChainID: 7, ShardID: 1, Epoch: 1, Genesis: "genesis"}
	authority, err := NewStakingQCAuthority(source, big.NewInt(1), domain)
	require.NoError(t, err)
	ids := authorityTestMemberIDs(keys)

	set := authority.NewVoteSet("one-atom", 1)
	addAuthorityTestVote(t, set, domain, ids[0], "one-atom", 1, secrets[0])
	qc, formed, err := set.QC()
	require.NoError(t, err)
	require.True(t, formed)
	require.Equal(t, []hotstuff.MemberID{ids[0]}, qc.QC.Signers)
	_, err = authority.Verify(qc)
	require.NoError(t, err)
}

func TestStakingQCAuthorityRejectsRoundedThresholdAcceptedByIntegerQuorum(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.ZeroDec())
	keys, secrets := authorityTestKeys(t, 2)
	threshold := numeric.NewDecFromBigInt(big.NewInt(666666666666666667))
	remainder := numeric.NewDecFromBigInt(big.NewInt(333333333333333333))
	source := &shard.Committee{ShardID: 1, Slots: shard.SlotList{
		{EcdsaAddress: common.HexToAddress("0x13"), BLSPublicKey: keys[0], EffectiveStake: &threshold},
		{EcdsaAddress: common.HexToAddress("0x14"), BLSPublicKey: keys[1], EffectiveStake: &remainder},
	}}
	domain := hotstuff.VoteDomain{ChainID: 7, ShardID: 1, Epoch: 1, Genesis: "genesis"}
	authority, err := NewStakingQCAuthority(source, big.NewInt(1), domain)
	require.NoError(t, err)
	ids := authorityTestMemberIDs(keys)

	exact := authority.NewVoteSet("threshold", 1)
	addAuthorityTestVote(t, exact, domain, ids[0], "threshold", 1, secrets[0])
	_, formed, err := exact.QC()
	require.NoError(t, err)
	require.False(t, formed)

	integerCommittee, err := hotstuff.NewBLSCommitteeFromValidatedKeys([]hotstuff.BLSMember{
		{
			Member:    hotstuff.Member{ID: ids[0], Power: 666666666666666667},
			PublicKey: hmybls.PublicKeyWrapper{Bytes: keys[0]},
		},
		{
			Member:    hotstuff.Member{ID: ids[1], Power: 333333333333333333},
			PublicKey: hmybls.PublicKeyWrapper{Bytes: keys[1]},
		},
	})
	require.NoError(t, err)
	integer := hotstuff.NewBLSVoteSet(integerCommittee, "threshold", 1, domain)
	addAuthorityTestVote(t, integer, domain, ids[0], "threshold", 1, secrets[0])
	certificate, formed, err := integer.QC()
	require.NoError(t, err)
	require.True(t, formed, "integer quorum rounds the same atoms differently")
	_, err = authority.Verify(certificate)
	require.ErrorIs(t, err, hotstuff.ErrInsufficientVotingPower)
}

func TestStakingQCAuthorityUsesOneAtomBoundaryForTCFormationAndVerification(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.ZeroDec())
	keys, secrets := authorityTestKeys(t, 2)
	above := numeric.NewDecFromBigInt(big.NewInt(666666666666666668))
	aboveRemainder := numeric.NewDecFromBigInt(big.NewInt(333333333333333332))
	threshold := numeric.NewDecFromBigInt(big.NewInt(666666666666666667))
	thresholdRemainder := numeric.NewDecFromBigInt(big.NewInt(333333333333333333))
	domain := hotstuff.VoteDomain{ChainID: 7, ShardID: 1, Epoch: 1, Genesis: "genesis"}
	aboveSource := &shard.Committee{ShardID: 1, Slots: shard.SlotList{
		{EcdsaAddress: common.HexToAddress("0x15"), BLSPublicKey: keys[0], EffectiveStake: &above},
		{EcdsaAddress: common.HexToAddress("0x16"), BLSPublicKey: keys[1], EffectiveStake: &aboveRemainder},
	}}
	thresholdSource := &shard.Committee{ShardID: 1, Slots: shard.SlotList{
		{EcdsaAddress: common.HexToAddress("0x15"), BLSPublicKey: keys[0], EffectiveStake: &threshold},
		{EcdsaAddress: common.HexToAddress("0x16"), BLSPublicKey: keys[1], EffectiveStake: &thresholdRemainder},
	}}
	aboveAuthority, err := NewStakingQCAuthority(aboveSource, big.NewInt(1), domain)
	require.NoError(t, err)
	thresholdAuthority, err := NewStakingQCAuthority(thresholdSource, big.NewInt(1), domain)
	require.NoError(t, err)
	_, aboveGenesis, err := aboveAuthority.NewCore(hotstuff.Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	_, thresholdGenesis, err := thresholdAuthority.NewCore(hotstuff.Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	id := HarmonyQuorumMemberID(keys[0])

	aboveSet := hotstuff.NewBLSTimeoutSet(aboveAuthority, 1)
	addAuthorityTestTimeout(
		t, aboveSet, domain, id, 1, aboveGenesis.QC(),
		hotstuff.BLSQC{QC: aboveGenesis.QC()}, secrets[0],
	)
	certificate, formed, err := aboveSet.Certificate()
	require.NoError(t, err)
	require.True(t, formed)
	_, err = aboveAuthority.VerifyTC(certificate)
	require.NoError(t, err)

	thresholdSet := hotstuff.NewBLSTimeoutSet(thresholdAuthority, 1)
	addAuthorityTestTimeout(
		t, thresholdSet, domain, id, 1, thresholdGenesis.QC(),
		hotstuff.BLSQC{QC: thresholdGenesis.QC()}, secrets[0],
	)
	_, formed, err = thresholdSet.Certificate()
	require.NoError(t, err)
	require.False(t, formed)
	_, err = thresholdAuthority.VerifyTC(certificate)
	require.ErrorIs(t, err, hotstuff.ErrInsufficientVotingPower)
}

func TestStakingQCAuthorityUsesExactHarmonyQuorumForTCFormationAndVerification(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.MustNewDecFromStr("0.4"))
	keys, secrets := authorityTestKeys(t, 4)
	stakeOne := numeric.NewDec(1)
	stakeTwo := numeric.NewDec(2)
	stakeThree := numeric.NewDec(3)
	source := &shard.Committee{ShardID: 2, Slots: shard.SlotList{
		{EcdsaAddress: common.HexToAddress("0x21"), BLSPublicKey: keys[0]},
		{EcdsaAddress: common.HexToAddress("0x22"), BLSPublicKey: keys[1], EffectiveStake: &stakeOne},
		{EcdsaAddress: common.HexToAddress("0x23"), BLSPublicKey: keys[2], EffectiveStake: &stakeTwo},
		{EcdsaAddress: common.HexToAddress("0x24"), BLSPublicKey: keys[3], EffectiveStake: &stakeThree},
	}}
	domain := hotstuff.VoteDomain{ChainID: 7, ShardID: 2, Epoch: 1, Genesis: "genesis"}
	authority, err := NewStakingQCAuthority(source, big.NewInt(1), domain)
	require.NoError(t, err)
	_, genesis, err := authority.NewCore(hotstuff.Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	ids := authorityTestMemberIDs(keys)
	evidence := hotstuff.BLSQC{QC: genesis.QC()}

	lowStake := hotstuff.NewBLSTimeoutSet(authority, 1)
	for _, index := range []int{1, 2, 3} {
		addAuthorityTestTimeout(t, lowStake, domain, ids[index], 1, genesis.QC(), evidence, secrets[index])
	}
	_, formed, err := lowStake.Certificate()
	require.NoError(t, err)
	require.False(t, formed, "three of four slots have only 0.6 EPoS power")

	highStake := hotstuff.NewBLSTimeoutSet(authority, 1)
	for _, index := range []int{0, 3} {
		addAuthorityTestTimeout(t, highStake, domain, ids[index], 1, genesis.QC(), evidence, secrets[index])
	}
	certificate, formed, err := highStake.Certificate()
	require.NoError(t, err)
	require.True(t, formed, "two of four slots have 0.7 EPoS power")
	require.Equal(t, []hotstuff.MemberID{ids[0], ids[3]}, certificate.Signers)
	require.Equal(t, []byte{0b00001001}, certificate.Bitmap)
	verified, err := authority.VerifyTC(certificate)
	require.NoError(t, err)
	badBitmap := certificate
	badBitmap.Bitmap = append([]byte(nil), certificate.Bitmap...)
	badBitmap.Bitmap[0] |= 1 << 7
	_, err = authority.VerifyTC(badBitmap)
	require.ErrorIs(t, err, hotstuff.ErrInvalidTCBitmap)
	tampered := certificate
	tampered.Signers = append([]hotstuff.MemberID(nil), ids[1], ids[2], ids[3])
	tampered.Bitmap = []byte{0b00001110}
	_, err = authority.VerifyTC(tampered)
	require.ErrorIs(t, err, hotstuff.ErrInsufficientVotingPower)
	pacemaker, err := authority.NewPacemaker(1)
	require.NoError(t, err)
	require.NoError(t, authority.AdvanceTimeout(pacemaker, verified))
	require.Equal(t, hotstuff.View(2), pacemaker.CurrentView())
}

func TestStakingQCAuthorityKeepsValidatorLeaderScheduleSeparateFromBLSQuorumSlots(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.ZeroDec())
	keys, _ := authorityTestKeys(t, 3)
	stake := numeric.NewDec(1)
	validatorA := common.HexToAddress("0xa1")
	validatorB := common.HexToAddress("0xb2")
	source := &shard.Committee{ShardID: 3, Slots: shard.SlotList{
		{EcdsaAddress: validatorA, BLSPublicKey: keys[0], EffectiveStake: &stake},
		{EcdsaAddress: validatorA, BLSPublicKey: keys[1], EffectiveStake: &stake},
		{EcdsaAddress: validatorB, BLSPublicKey: keys[2], EffectiveStake: &stake},
	}}
	domain := hotstuff.VoteDomain{ChainID: 7, ShardID: 3, Epoch: 1, Genesis: "genesis"}
	authority, err := NewStakingQCAuthority(source, big.NewInt(1), domain)
	require.NoError(t, err)
	source.Slots[0].EcdsaAddress = validatorB
	source.Slots[0].BLSPublicKey = hmybls.SerializedPublicKey{}
	source.Slots[1].EcdsaAddress = validatorB
	source.Slots[2].EcdsaAddress = validatorA
	_, _, err = authority.NewCore(hotstuff.Block{ID: "genesis", View: 0})
	require.NoError(t, err)

	for _, test := range []struct {
		view   hotstuff.View
		leader hotstuff.MemberID
	}{
		{view: 1, leader: hotstuff.MemberID(validatorA.Hex())},
		{view: 2, leader: hotstuff.MemberID(validatorB.Hex())},
		{view: 3, leader: hotstuff.MemberID(validatorA.Hex())},
		{view: 4, leader: hotstuff.MemberID(validatorB.Hex())},
	} {
		pacemaker, err := authority.NewPacemaker(test.view)
		require.NoError(t, err)
		require.Equal(t, test.leader, pacemaker.Leader())
	}
}

func TestStakingQCAuthorityRejectsDomainMismatch(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.ZeroDec())
	keys, _ := authorityTestKeys(t, 1)
	stake := numeric.NewDec(1)
	source := &shard.Committee{ShardID: 2, Slots: shard.SlotList{{
		EcdsaAddress: common.HexToAddress("0x31"), BLSPublicKey: keys[0], EffectiveStake: &stake,
	}}}

	_, err := NewStakingQCAuthority(
		source,
		big.NewInt(1),
		hotstuff.VoteDomain{ChainID: 7, ShardID: 3, Epoch: 1, Genesis: "genesis"},
	)
	require.ErrorIs(t, err, ErrQuorumDomainMismatch)
	_, err = NewStakingQCAuthority(
		source,
		big.NewInt(1),
		hotstuff.VoteDomain{ChainID: 7, ShardID: 2, Epoch: 2, Genesis: "genesis"},
	)
	require.ErrorIs(t, err, ErrQuorumDomainMismatch)
}

func TestStakingQCAuthorityRejectsMalformedStakeWithoutPanicking(t *testing.T) {
	useHarmonyQuorumSchedule(t, numeric.ZeroDec())
	keys, _ := authorityTestKeys(t, 1)
	malformed := numeric.Dec{}
	source := &shard.Committee{ShardID: 2, Slots: shard.SlotList{{
		EcdsaAddress: common.HexToAddress("0x41"), BLSPublicKey: keys[0], EffectiveStake: &malformed,
	}}}

	_, err := NewStakingQCAuthority(
		source,
		big.NewInt(1),
		hotstuff.VoteDomain{ChainID: 7, ShardID: 2, Epoch: 1, Genesis: "genesis"},
	)
	require.ErrorIs(t, err, ErrInvalidQuorumVotingPower)
}

func authorityTestKeys(t *testing.T, count int) ([]hmybls.SerializedPublicKey, []*bls_core.SecretKey) {
	t.Helper()
	keys := make([]hmybls.SerializedPublicKey, count)
	secrets := make([]*bls_core.SecretKey, count)
	for index := range keys {
		secret := hmybls.RandPrivateKey()
		wrapper := hmybls.WrapperFromPrivateKey(secret)
		keys[index] = wrapper.Pub.Bytes
		secrets[index] = secret
	}
	return keys, secrets
}

func authorityTestMemberIDs(keys []hmybls.SerializedPublicKey) []hotstuff.MemberID {
	ids := make([]hotstuff.MemberID, len(keys))
	for index, key := range keys {
		ids[index] = HarmonyQuorumMemberID(key)
	}
	return ids
}

func addAuthorityTestVote(
	t *testing.T,
	set *hotstuff.BLSVoteSet,
	domain hotstuff.VoteDomain,
	voter hotstuff.MemberID,
	block hotstuff.BlockID,
	view hotstuff.View,
	secret *bls_core.SecretKey,
) {
	t.Helper()
	signed, err := hotstuff.SignVote(domain, hotstuff.Vote{Voter: voter, Block: block, View: view}, secret)
	require.NoError(t, err)
	require.NoError(t, set.Add(signed))
}

func addAuthorityTestTimeout(
	t *testing.T,
	set *hotstuff.BLSTimeoutSet,
	domain hotstuff.VoteDomain,
	voter hotstuff.MemberID,
	view hotstuff.View,
	highQC hotstuff.QC,
	evidence hotstuff.BLSQC,
	secret *bls_core.SecretKey,
) {
	t.Helper()
	signed, err := hotstuff.SignTimeout(
		domain,
		hotstuff.Timeout{Voter: voter, View: view, HighQC: highQC},
		secret,
	)
	require.NoError(t, err)
	require.NoError(t, set.Add(signed, evidence))
}
