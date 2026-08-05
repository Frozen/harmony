package hotstuff

import (
	"testing"

	bls_core "github.com/harmony-one/harmony/crypto/bls/core"
	"github.com/stretchr/testify/require"
)

func TestVerifiedQCGatesCoreAndPacemaker(t *testing.T) {
	members := []Member{
		{ID: "alice", Power: 1},
		{ID: "bob", Power: 1},
		{ID: "carol", Power: 1},
		{ID: "dave", Power: 1},
	}
	committee, secrets := testBLSCommittee(t, members...)
	domain := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	authority := NewQCAuthority(committee, domain)
	core, genesis, err := authority.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)

	b1 := Block{ID: "b1", Parent: "genesis", View: 1, Justify: genesis.QC()}
	_, err = authority.Accept(core, b1, genesis)
	require.NoError(t, err)

	certificate := formBLSQC(t, committee, secrets, domain, "b1", 1)
	verified, err := authority.Verify(certificate)
	require.NoError(t, err)

	pacemaker, err := authority.NewPacemaker(1)
	require.NoError(t, err)
	require.NoError(t, authority.Advance(pacemaker, verified))
	require.Equal(t, View(2), pacemaker.CurrentView())
	require.Equal(t, QC{Block: "b1", View: 1, Signers: []MemberID{"alice", "bob", "carol"}}, pacemaker.HighQC())

	b2 := Block{ID: "b2", Parent: "b1", View: 2, Justify: verified.QC()}
	_, err = authority.Accept(core, b2, verified)
	require.NoError(t, err)
}

func TestAcceptCertifiedRejectsProofForDifferentQC(t *testing.T) {
	committee, secrets := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	domain := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	authority := NewQCAuthority(committee, domain)
	core, genesis, err := authority.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	_, err = authority.Accept(
		core,
		Block{ID: "b1", Parent: "genesis", View: 1, Justify: genesis.QC()},
		genesis,
	)
	require.NoError(t, err)

	certificate := formBLSQC(t, committee, secrets, domain, "b1", 1)
	verified, err := authority.Verify(certificate)
	require.NoError(t, err)
	mismatch := Block{ID: "b2", Parent: "b1", View: 2, Justify: QC{Block: "mallory", View: 1}}

	_, err = authority.Accept(core, mismatch, verified)
	require.ErrorIs(t, err, ErrCertifiedQCMismatch)
}

func TestForgedBLSQCCannotBecomeVerifiedQC(t *testing.T) {
	committee, secrets := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	domain := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	authority := NewQCAuthority(committee, domain)
	certificate := formBLSQC(t, committee, secrets, domain, "b1", 1)
	certificate.Signature[0] ^= 0xff
	_, _, err := authority.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	pacemaker, err := authority.NewPacemaker(1)
	require.NoError(t, err)

	_, err = authority.Verify(certificate)
	require.Error(t, err)
	require.Equal(t, View(1), pacemaker.CurrentView())
	require.Equal(t, QC{Block: "genesis", View: 0}, pacemaker.HighQC())
}

func TestAuthorityCannotDriveStateMachinesBoundToAnotherDomain(t *testing.T) {
	committee, secrets := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	domainA := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	domainB := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 43, Genesis: "genesis"}
	authorityA := NewQCAuthority(committee, domainA)
	authorityB := NewQCAuthority(committee, domainB)

	certificateB := formBLSQC(t, committee, secrets, domainB, "b1", 1)
	verifiedB, err := authorityB.Verify(certificateB)
	require.NoError(t, err)
	_, _, err = authorityA.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	pacemakerA, err := authorityA.NewPacemaker(1)
	require.NoError(t, err)
	require.ErrorIs(t, authorityB.Advance(pacemakerA, verifiedB), ErrWrongQCAuthority)
	require.Equal(t, View(1), pacemakerA.CurrentView())

	coreA, _, err := authorityA.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	_, genesisB, err := authorityB.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	_, err = authorityB.Accept(
		coreA,
		Block{ID: "b1", Parent: "genesis", View: 1, Justify: genesisB.QC()},
		genesisB,
	)
	require.ErrorIs(t, err, ErrWrongQCAuthority)
	require.Equal(t, BlockID("genesis"), coreA.Committed())
}

func TestVerifiedQCOwnsCertificateEvidence(t *testing.T) {
	committee, secrets := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	domain := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	authority := NewQCAuthority(committee, domain)
	certificate := formBLSQC(t, committee, secrets, domain, "b1", 1)
	verified, err := authority.Verify(certificate)
	require.NoError(t, err)

	certificate.QC.Signers[0] = "mallory"
	first := verified.QC()
	require.Equal(t, []MemberID{"alice", "bob", "carol"}, first.Signers)
	first.Signers[0] = "mallory"
	require.Equal(t, []MemberID{"alice", "bob", "carol"}, verified.QC().Signers)
}

func TestQCAuthorityDoesNotTrustNonzeroViewGenesis(t *testing.T) {
	committee, _ := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	authority := NewQCAuthority(committee, VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"})
	_, _, err := authority.NewCore(Block{ID: "fake-genesis", View: 7})
	require.ErrorIs(t, err, ErrInvalidGenesis)
}

func TestQCAuthorityRejectsSecondGenesisRoot(t *testing.T) {
	committee, _ := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	authority := NewQCAuthority(committee, VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"})
	_, genesis, err := authority.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	pacemaker, err := authority.NewPacemaker(1)
	require.NoError(t, err)
	require.Equal(t, genesis.QC(), pacemaker.HighQC())

	_, _, err = authority.NewCore(Block{ID: "attacker-chosen", View: 0})
	require.ErrorIs(t, err, ErrGenesisRootMismatch)
	require.Equal(t, BlockID("genesis"), pacemaker.HighQC().Block)
}

func TestQCAuthorityRequiresGenesisBeforePacemaker(t *testing.T) {
	committee, _ := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	authority := NewQCAuthority(committee, VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"})
	_, err := authority.NewPacemaker(1)
	require.ErrorIs(t, err, ErrMissingGenesisRoot)

	_, _, err = authority.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	_, err = authority.NewPacemaker(0)
	require.ErrorIs(t, err, ErrInvalidInitialView)
}

func TestPacemakerRejectsQCFromForeignGenesis(t *testing.T) {
	committee, secrets := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	domain := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	authority := NewQCAuthority(committee, domain)
	_, _, err := authority.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	pacemaker, err := authority.NewPacemaker(1)
	require.NoError(t, err)

	foreignDomain := domain
	foreignDomain.Genesis = "foreign-genesis"
	foreignQC := formBLSQC(t, committee, secrets, foreignDomain, "foreign-root-block", 1)
	_, err = authority.Verify(foreignQC)
	require.ErrorIs(t, err, ErrInvalidQCSignature)
	require.Equal(t, QC{Block: "genesis", View: 0}, pacemaker.HighQC())
}

func formBLSQC(
	t *testing.T,
	committee *BLSCommittee,
	secrets map[MemberID]*bls_core.SecretKey,
	domain VoteDomain,
	block BlockID,
	view View,
) BLSQC {
	t.Helper()
	set := NewBLSVoteSet(committee, block, view, domain)
	for _, voter := range []MemberID{"carol", "alice", "bob"} {
		signed, err := SignVote(domain, Vote{Voter: voter, Block: block, View: view}, secrets[voter])
		require.NoError(t, err)
		require.NoError(t, set.Add(signed))
	}
	qc, formed, err := set.QC()
	require.NoError(t, err)
	require.True(t, formed)
	return qc
}
