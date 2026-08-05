package hotstuff

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBroadcastTimeoutsProduceEquivalentCertificatesAtEveryReplica(t *testing.T) {
	committee := testCommittee(t, "alice", "bob", "carol", "dave")
	timeouts := map[MemberID]Timeout{
		"alice": {Voter: "alice", View: 3, HighQC: certifiedQC("b1", 1, "alice", "bob", "carol")},
		"bob":   {Voter: "bob", View: 3, HighQC: certifiedQC("b2", 2, "carol", "bob", "alice")},
		"carol": {Voter: "carol", View: 3, HighQC: certifiedQC("b1", 1, "alice", "bob", "carol")},
		"dave":  {Voter: "dave", View: 3, HighQC: certifiedQC("b2", 2, "alice", "bob", "carol")},
	}
	quorums := [][]MemberID{
		{"alice", "bob", "carol"},
		{"bob", "carol", "dave"},
	}

	certificates := make([]TimeoutCertificate, 0, len(quorums))
	for _, quorum := range quorums {
		set := NewTimeoutSet(committee, 3)
		for _, voter := range quorum {
			require.NoError(t, set.Add(timeouts[voter]))
		}
		certificate, ok, err := set.Certificate()
		require.NoError(t, err)
		require.True(t, ok)
		require.NoError(t, committee.requireQuorum(certificate.Signers))
		certificates = append(certificates, certificate)
	}

	require.Equal(t, View(3), certificates[0].View)
	require.Equal(t, certificates[0].View, certificates[1].View)
	require.Equal(t, certifiedQC("b2", 2, "alice", "bob", "carol"), certificates[0].HighQC)
	require.Equal(t, certificates[0].HighQC, certificates[1].HighQC)
	require.NotEqual(t, certificates[0].Signers, certificates[1].Signers,
		"valid certificates may carry different quorum witnesses")
}

func TestPacemakerTimeoutChangesLeaderWithoutProducingBlock(t *testing.T) {
	committee := testCommittee(t, "alice", "bob", "carol")
	pacemaker := newPacemaker(committee, 3)
	require.Equal(t, MemberID("carol"), pacemaker.Leader())

	err := pacemaker.advanceTimeout(TimeoutCertificate{
		View:    3,
		HighQC:  certifiedQC("b2", 2, "alice", "bob", "carol"),
		Signers: []MemberID{"alice", "bob", "carol"},
	})
	require.NoError(t, err)
	require.Equal(t, View(4), pacemaker.CurrentView())
	require.Equal(t, MemberID("alice"), pacemaker.Leader())
	require.Equal(t, certifiedQC("b2", 2, "alice", "bob", "carol"), pacemaker.HighQC())
}

func TestPacemakerQCAdvancesToNextLeader(t *testing.T) {
	committee := testCommittee(t, "alice", "bob", "carol")
	pacemaker := newPacemaker(committee, 1)

	err := pacemaker.advanceQC(certifiedQC("b1", 1, "carol", "alice", "bob"))
	require.NoError(t, err)
	require.Equal(t, View(2), pacemaker.CurrentView())
	require.Equal(t, MemberID("bob"), pacemaker.Leader())
	require.Equal(t, certifiedQC("b1", 1, "alice", "bob", "carol"), pacemaker.HighQC())
}

func TestPacemakerRejectsStaleAndUnderpoweredCertificates(t *testing.T) {
	committee := testCommittee(t, "alice", "bob", "carol", "dave")
	pacemaker := newPacemaker(committee, 4)

	err := pacemaker.advanceTimeout(TimeoutCertificate{
		View:    3,
		Signers: []MemberID{"alice", "bob", "carol"},
	})
	require.ErrorIs(t, err, ErrStaleCertificate)

	err = pacemaker.advanceTimeout(TimeoutCertificate{
		View:    4,
		Signers: []MemberID{"alice", "bob"},
	})
	require.ErrorIs(t, err, ErrInsufficientVotingPower)
	require.Equal(t, View(4), pacemaker.CurrentView())
}

func TestPacemakerRejectsFutureTimeoutCertificate(t *testing.T) {
	committee := testCommittee(t, "alice", "bob", "carol")
	pacemaker := newPacemaker(committee, 4)

	err := pacemaker.advanceTimeout(TimeoutCertificate{
		View:    6,
		HighQC:  certifiedQC("b5", 5, "alice", "bob", "carol"),
		Signers: []MemberID{"alice", "bob", "carol"},
	})
	require.ErrorIs(t, err, ErrWrongCertificateView)
	require.Equal(t, View(4), pacemaker.CurrentView())
}

func TestTimeoutPathRejectsUnderpoweredHighQC(t *testing.T) {
	committee := testCommittee(t, "alice", "bob", "carol", "dave")
	underpowered := certifiedQC("b2", 2, "alice", "bob")

	set := NewTimeoutSet(committee, 3)
	err := set.Add(Timeout{Voter: "alice", View: 3, HighQC: underpowered})
	require.ErrorIs(t, err, ErrInsufficientVotingPower)

	pacemaker := newPacemaker(committee, 3)
	err = pacemaker.advanceTimeout(TimeoutCertificate{
		View:    3,
		HighQC:  underpowered,
		Signers: []MemberID{"alice", "bob", "carol"},
	})
	require.ErrorIs(t, err, ErrInsufficientVotingPower)
	require.Equal(t, View(3), pacemaker.CurrentView())
	require.Equal(t, QC{}, pacemaker.HighQC())
}

func TestPacemakerOwnsHighQCEvidence(t *testing.T) {
	committee := testCommittee(t, "alice", "bob", "carol")
	pacemaker := newPacemaker(committee, 1)
	qc := certifiedQC("b1", 1, "alice", "bob", "carol")

	require.NoError(t, pacemaker.advanceQC(qc))
	qc.Signers[0] = "mallory"
	state := pacemaker.HighQC()
	state.Signers[0] = "mallory"

	require.Equal(t, certifiedQC("b1", 1, "alice", "bob", "carol"), pacemaker.HighQC())
}

func TestTimeoutSetRejectsDuplicateAndWrongView(t *testing.T) {
	committee := testCommittee(t, "alice", "bob", "carol")
	set := NewTimeoutSet(committee, 2)
	timeout := Timeout{
		Voter:  "alice",
		View:   2,
		HighQC: certifiedQC("b1", 1, "alice", "bob", "carol"),
	}

	require.NoError(t, set.Add(timeout))
	require.ErrorIs(t, set.Add(timeout), ErrDuplicateTimeout)
	require.ErrorIs(t, set.Add(Timeout{Voter: "bob", View: 3}), ErrWrongTimeoutView)
	require.ErrorIs(t, set.Add(Timeout{Voter: "mallory", View: 2}), ErrUnknownVoter)
}

func testCommittee(t *testing.T, ids ...MemberID) *Committee {
	t.Helper()
	members := make([]Member, 0, len(ids))
	for _, id := range ids {
		members = append(members, Member{ID: id, Power: 1})
	}
	committee, err := NewCommittee(members)
	require.NoError(t, err)
	return committee
}

func certifiedQC(block BlockID, view View, signers ...MemberID) QC {
	return QC{Block: block, View: view, Signers: signers}
}
