package hotstuff_test

import (
	"testing"

	"github.com/harmony-one/harmony/consensus/hotstuff"
	hmybls "github.com/harmony-one/harmony/crypto/bls"
	"github.com/stretchr/testify/require"
)

type externalCertificateQuorum struct{}

func (externalCertificateQuorum) HasQuorum(signers []hotstuff.MemberID) (bool, error) {
	return len(signers) >= 1, nil
}

type externalLeaderSchedule struct{}

func (externalLeaderSchedule) Leader(hotstuff.View) hotstuff.MemberID {
	return "validator"
}

func TestAuthorityConstructsUsableExternalStateMachines(t *testing.T) {
	members := make([]hotstuff.BLSMember, 0, 3)
	for _, id := range []hotstuff.MemberID{"alice", "bob", "carol"} {
		wrapper := hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey())
		members = append(members, hotstuff.BLSMember{
			Member:    hotstuff.Member{ID: id, Power: 1},
			PublicKey: *wrapper.Pub,
		})
	}
	committee, err := hotstuff.NewBLSCommitteeFromValidatedKeys(members)
	require.NoError(t, err)
	authority := hotstuff.NewQCAuthority(
		committee,
		hotstuff.VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"},
	)
	core, genesis, err := authority.NewCore(hotstuff.Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	_, err = authority.Accept(core, hotstuff.Block{
		ID:      "b1",
		Parent:  "genesis",
		View:    1,
		Justify: genesis.QC(),
	}, genesis)
	require.NoError(t, err)

	pacemaker, err := authority.NewPacemaker(1)
	require.NoError(t, err)
	require.Equal(t, hotstuff.View(1), pacemaker.CurrentView())
	require.Equal(t, hotstuff.BlockID("genesis"), pacemaker.HighQC().Block)

	_, _, err = authority.NewCore(hotstuff.Block{ID: "attacker-chosen", View: 0})
	require.ErrorIs(t, err, hotstuff.ErrGenesisRootMismatch)
	require.Equal(t, hotstuff.BlockID("genesis"), pacemaker.HighQC().Block)
}

func TestExternalAuthorityUsesPluggableQuorumAndLeaderSchedule(t *testing.T) {
	secret := hmybls.RandPrivateKey()
	committee, err := hotstuff.NewBLSCommitteeFromValidatedKeysWithQuorum(
		[]hotstuff.BLSMember{{
			Member:    hotstuff.Member{ID: "slot", Power: 1},
			PublicKey: *hmybls.WrapperFromPrivateKey(secret).Pub,
		}},
		externalCertificateQuorum{},
	)
	require.NoError(t, err)
	domain := hotstuff.VoteDomain{Genesis: "genesis"}
	authority, err := hotstuff.NewQCAuthorityWithLeaderSchedule(
		committee, domain, externalLeaderSchedule{},
	)
	require.NoError(t, err)
	_, _, err = authority.NewCore(hotstuff.Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	pacemaker, err := authority.NewPacemaker(1)
	require.NoError(t, err)
	require.Equal(t, hotstuff.MemberID("validator"), pacemaker.Leader())

	votes := authority.NewVoteSet("b1", 1)
	signed, err := hotstuff.SignVote(
		domain, hotstuff.Vote{Voter: "slot", Block: "b1", View: 1}, secret,
	)
	require.NoError(t, err)
	require.NoError(t, votes.Add(signed))
	certificate, formed, err := votes.QC()
	require.NoError(t, err)
	require.True(t, formed)
	_, err = authority.Verify(certificate)
	require.NoError(t, err)
}
