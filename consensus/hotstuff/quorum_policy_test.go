package hotstuff

import (
	"errors"
	"testing"

	hmybls "github.com/harmony-one/harmony/crypto/bls"
	bls_core "github.com/harmony-one/harmony/crypto/bls/core"
	"github.com/stretchr/testify/require"
)

type testCertificateQuorum struct {
	hasQuorum bool
	err       error
	mutate    bool
	setupSize int
}

func (q *testCertificateQuorum) HasQuorum(signers []MemberID) (bool, error) {
	if q.setupSize > 0 && len(signers) == q.setupSize {
		return true, nil
	}
	if q.mutate && len(signers) > 0 {
		signers[0] = "mutated"
	}
	return q.hasQuorum, q.err
}

type testLeaderSchedule struct {
	leader MemberID
}

func (s *testLeaderSchedule) Leader(View) MemberID {
	return s.leader
}

func TestBLSCommitteeRejectsNilCertificateQuorum(t *testing.T) {
	members := testValidatedBLSMembers(t, Member{ID: "alice", Power: 1})

	_, err := NewBLSCommitteeFromValidatedKeysWithQuorum(members, nil)
	require.ErrorIs(t, err, ErrNilCertificateQuorum)
	var typedNil *testCertificateQuorum
	_, err = NewBLSCommitteeFromValidatedKeysWithQuorum(members, typedNil)
	require.ErrorIs(t, err, ErrNilCertificateQuorum)
}

func TestBLSCommitteeRejectsCertificateQuorumRosterMismatch(t *testing.T) {
	members := testValidatedBLSMembers(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
	)
	_, err := NewBLSCommitteeFromValidatedKeysWithQuorum(
		members, &testCertificateQuorum{hasQuorum: false},
	)
	require.ErrorIs(t, err, ErrCertificateQuorumRosterMismatch)

	policyErr := errors.New("test roster mismatch")
	_, err = NewBLSCommitteeFromValidatedKeysWithQuorum(
		members, &testCertificateQuorum{err: policyErr},
	)
	require.ErrorIs(t, err, policyErr)
}

func TestQCAuthorityRejectsNilLeaderSchedule(t *testing.T) {
	committee, _ := testBLSCommittee(t, Member{ID: "alice", Power: 1})
	domain := VoteDomain{Genesis: "genesis"}

	_, err := NewQCAuthorityWithLeaderSchedule(nil, domain, committee.committee)
	require.ErrorIs(t, err, ErrNilBLSCommittee)
	_, err = NewQCAuthorityWithLeaderSchedule(committee, domain, nil)
	require.ErrorIs(t, err, ErrNilLeaderSchedule)
	var typedNil *testLeaderSchedule
	_, err = NewQCAuthorityWithLeaderSchedule(committee, domain, typedNil)
	require.ErrorIs(t, err, ErrNilLeaderSchedule)
}

func TestCertificateQuorumPolicyErrorsFailClosedAndPropagate(t *testing.T) {
	policyErr := errors.New("test certificate quorum failure")
	policy := &testCertificateQuorum{err: policyErr, setupSize: 3}
	members, secrets := testValidatedBLSMembersAndSecrets(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	committee, err := NewBLSCommitteeFromValidatedKeysWithQuorum(members, policy)
	require.NoError(t, err)

	votes := NewVoteSet(committee.committee, "b1", 1)
	require.NoError(t, votes.Add(Vote{Voter: "alice", Block: "b1", View: 1}))
	_, formed, err := votes.QC()
	require.ErrorIs(t, err, policyErr)
	require.False(t, formed)

	timeouts := NewTimeoutSet(committee.committee, 1)
	require.NoError(t, timeouts.Add(Timeout{
		Voter: "alice", View: 1, HighQC: QC{Block: "genesis", View: 0},
	}))
	_, formed, err = timeouts.Certificate()
	require.ErrorIs(t, err, policyErr)
	require.False(t, formed)

	require.ErrorIs(t, committee.committee.requireQC(QC{
		Block: "b1", View: 1, Signers: []MemberID{"alice"},
	}), policyErr)
	pacemaker := newPacemaker(committee.committee, 1)
	require.ErrorIs(t, pacemaker.advanceQC(QC{
		Block: "b1", View: 1, Signers: []MemberID{"alice"},
	}), policyErr)

	domain := VoteDomain{Genesis: "genesis"}
	blsVotes := NewBLSVoteSet(committee, "b1", 1, domain)
	signedVote, err := SignVote(
		domain, Vote{Voter: "alice", Block: "b1", View: 1}, secrets["alice"],
	)
	require.NoError(t, err)
	require.NoError(t, blsVotes.Add(signedVote))
	_, formed, err = blsVotes.QC()
	require.ErrorIs(t, err, policyErr)
	require.False(t, formed)

	authority := NewQCAuthority(committee, domain)
	_, genesis, err := authority.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	blsTimeouts := NewBLSTimeoutSet(authority, 1)
	signedTimeout, err := SignTimeout(
		domain,
		Timeout{Voter: "alice", View: 1, HighQC: genesis.QC()},
		secrets["alice"],
	)
	require.NoError(t, err)
	require.NoError(t, blsTimeouts.Add(signedTimeout, BLSQC{QC: genesis.QC()}))
	_, formed, err = blsTimeouts.Certificate()
	require.ErrorIs(t, err, policyErr)
	require.False(t, formed)
}

func TestCertificateQuorumCannotMutateCanonicalSignerOutput(t *testing.T) {
	policy := &testCertificateQuorum{hasQuorum: true, mutate: true}
	members := testValidatedBLSMembers(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
	)
	committee, err := NewBLSCommitteeFromValidatedKeysWithQuorum(members, policy)
	require.NoError(t, err)
	votes := NewVoteSet(committee.committee, "b1", 1)
	require.NoError(t, votes.Add(Vote{Voter: "alice", Block: "b1", View: 1}))

	qc, formed, err := votes.QC()
	require.NoError(t, err)
	require.True(t, formed)
	require.Equal(t, []MemberID{"alice"}, qc.Signers)

	input := []MemberID{"alice"}
	require.NoError(t, committee.committee.requireQuorum(input))
	require.Equal(t, []MemberID{"alice"}, input)
}

func TestQCAuthorityUsesSeparateLeaderScheduleWithoutChangingDefault(t *testing.T) {
	committee, _ := testBLSCommittee(t,
		Member{ID: "slot-a", Power: 1},
		Member{ID: "slot-b", Power: 1},
	)
	domain := VoteDomain{Genesis: "genesis"}
	custom, err := NewQCAuthorityWithLeaderSchedule(
		committee, domain, &testLeaderSchedule{leader: "validator"},
	)
	require.NoError(t, err)
	_, _, err = custom.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	pacemaker, err := custom.NewPacemaker(1)
	require.NoError(t, err)
	require.Equal(t, MemberID("validator"), pacemaker.Leader())

	legacy := NewQCAuthority(committee, domain)
	_, _, err = legacy.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	pacemaker, err = legacy.NewPacemaker(1)
	require.NoError(t, err)
	require.Equal(t, MemberID("slot-a"), pacemaker.Leader())
}

func testValidatedBLSMembers(t *testing.T, members ...Member) []BLSMember {
	t.Helper()
	result, _ := testValidatedBLSMembersAndSecrets(t, members...)
	return result
}

func testValidatedBLSMembersAndSecrets(
	t *testing.T,
	members ...Member,
) ([]BLSMember, map[MemberID]*bls_core.SecretKey) {
	t.Helper()
	result := make([]BLSMember, len(members))
	secrets := make(map[MemberID]*bls_core.SecretKey, len(members))
	for index, member := range members {
		secret := hmybls.RandPrivateKey()
		secrets[member.ID] = secret
		result[index] = BLSMember{
			Member:    member,
			PublicKey: *hmybls.WrapperFromPrivateKey(secret).Pub,
		}
	}
	return result, secrets
}
