package hotstuff

import (
	"testing"

	hmybls "github.com/harmony-one/harmony/crypto/bls"
	bls_core "github.com/harmony-one/harmony/crypto/bls/core"
	"github.com/stretchr/testify/require"
)

func TestBLSSignedBroadcastVotesFormVerifiableQC(t *testing.T) {
	committee, secrets := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
		Member{ID: "dave", Power: 1},
	)
	domain := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	set := NewBLSVoteSet(committee, "b7", 7, domain)

	for _, voter := range []MemberID{"carol", "alice", "bob"} {
		signed, err := SignVote(domain, Vote{Voter: voter, Block: "b7", View: 7}, secrets[voter])
		require.NoError(t, err)
		require.NoError(t, set.Add(signed))
	}

	qc, formed, err := set.QC()
	require.NoError(t, err)
	require.True(t, formed)
	require.Equal(t, QC{
		Block:   "b7",
		View:    7,
		Signers: []MemberID{"alice", "bob", "carol"},
	}, qc.QC)
	require.Len(t, qc.Signature, hmybls.BLSSignatureSizeInBytes)
	require.Equal(t, []byte{0b00000111}, qc.Bitmap)
	require.NoError(t, committee.VerifyQC(domain, qc))
}

func TestBLSSignedVoteCannotBeReplayedAcrossDomains(t *testing.T) {
	committee, secrets := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	vote := Vote{Voter: "alice", Block: "b7", View: 7}
	signed, err := SignVote(VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}, vote, secrets["alice"])
	require.NoError(t, err)

	otherEpoch := NewBLSVoteSet(
		committee, "b7", 7,
		VoteDomain{ChainID: 1, ShardID: 0, Epoch: 43, Genesis: "genesis"},
	)
	require.ErrorIs(t, otherEpoch.Add(signed), ErrInvalidVoteSignature)

	otherGenesis := NewBLSVoteSet(
		committee, "b7", 7,
		VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "foreign-genesis"},
	)
	require.ErrorIs(t, otherGenesis.Add(signed), ErrInvalidVoteSignature)
}

func TestBLSQCVerificationRejectsTampering(t *testing.T) {
	committee, secrets := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	domain := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	set := NewBLSVoteSet(committee, "b7", 7, domain)
	for _, voter := range []MemberID{"alice", "bob", "carol"} {
		signed, err := SignVote(domain, Vote{Voter: voter, Block: "b7", View: 7}, secrets[voter])
		require.NoError(t, err)
		require.NoError(t, set.Add(signed))
	}
	qc, formed, err := set.QC()
	require.NoError(t, err)
	require.True(t, formed)

	tamperedBlock := cloneBLSQC(qc)
	tamperedBlock.QC.Block = "mallory"
	require.ErrorIs(t, committee.VerifyQC(domain, tamperedBlock), ErrInvalidQCSignature)

	tamperedBitmap := cloneBLSQC(qc)
	tamperedBitmap.Bitmap[0] ^= 1 << 3
	require.ErrorIs(t, committee.VerifyQC(domain, tamperedBitmap), ErrInvalidQCBitmap)

	reorderedSigners := cloneBLSQC(qc)
	reorderedSigners.QC.Signers[0], reorderedSigners.QC.Signers[1] =
		reorderedSigners.QC.Signers[1], reorderedSigners.QC.Signers[0]
	require.ErrorIs(t, committee.VerifyQC(domain, reorderedSigners), ErrNonCanonicalQCSigners)
}

func TestBLSVoteSetUsesWeightedQuorum(t *testing.T) {
	committee, secrets := testBLSCommittee(t,
		Member{ID: "alice", Power: 3},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
		Member{ID: "dave", Power: 1},
	)
	domain := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	set := NewBLSVoteSet(committee, "b7", 7, domain)

	for _, voter := range []MemberID{"alice", "bob"} {
		signed, err := SignVote(domain, Vote{Voter: voter, Block: "b7", View: 7}, secrets[voter])
		require.NoError(t, err)
		require.NoError(t, set.Add(signed))
	}
	_, formed, err := set.QC()
	require.NoError(t, err)
	require.False(t, formed, "four of six voting power is not strictly above two thirds")

	signed, err := SignVote(domain, Vote{Voter: "carol", Block: "b7", View: 7}, secrets["carol"])
	require.NoError(t, err)
	require.NoError(t, set.Add(signed))
	qc, formed, err := set.QC()
	require.NoError(t, err)
	require.True(t, formed)
	require.NoError(t, committee.VerifyQC(domain, qc))
}

func TestBLSCommitteeRejectsDuplicatePublicKey(t *testing.T) {
	secret := hmybls.RandPrivateKey()
	wrapper := hmybls.WrapperFromPrivateKey(secret)
	_, err := NewBLSCommitteeFromValidatedKeys([]BLSMember{
		{Member: Member{ID: "alice", Power: 1}, PublicKey: *wrapper.Pub},
		{Member: Member{ID: "bob", Power: 1}, PublicKey: *wrapper.Pub},
	})
	require.ErrorIs(t, err, ErrDuplicateBLSPublicKey)
}

func cloneBLSQC(qc BLSQC) BLSQC {
	return BLSQC{
		QC:        cloneQC(qc.QC),
		Signature: append([]byte(nil), qc.Signature...),
		Bitmap:    append([]byte(nil), qc.Bitmap...),
	}
}

func testBLSCommittee(t *testing.T, members ...Member) (*BLSCommittee, map[MemberID]*bls_core.SecretKey) {
	t.Helper()
	blsMembers := make([]BLSMember, 0, len(members))
	secrets := make(map[MemberID]*bls_core.SecretKey, len(members))
	for _, member := range members {
		secret := hmybls.RandPrivateKey()
		wrapper := hmybls.WrapperFromPrivateKey(secret)
		blsMembers = append(blsMembers, BLSMember{Member: member, PublicKey: *wrapper.Pub})
		secrets[member.ID] = secret
	}
	committee, err := NewBLSCommitteeFromValidatedKeys(blsMembers)
	require.NoError(t, err)
	return committee, secrets
}
