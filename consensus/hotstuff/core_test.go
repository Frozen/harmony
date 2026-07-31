package hotstuff

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoundRobinLeaderChangesEveryView(t *testing.T) {
	committee, err := NewCommittee([]Member{
		{ID: "alice", Power: 1},
		{ID: "bob", Power: 1},
		{ID: "carol", Power: 1},
	})
	require.NoError(t, err)

	require.Equal(t, MemberID("alice"), committee.Leader(1))
	require.Equal(t, MemberID("bob"), committee.Leader(2))
	require.Equal(t, MemberID("carol"), committee.Leader(3))
	require.Equal(t, MemberID("alice"), committee.Leader(4))
}

func TestBroadcastVotesProduceSameQCAtEveryReplica(t *testing.T) {
	committee, err := NewCommittee([]Member{
		{ID: "alice", Power: 1},
		{ID: "bob", Power: 1},
		{ID: "carol", Power: 1},
		{ID: "dave", Power: 1},
	})
	require.NoError(t, err)

	votes := []Vote{
		{Voter: "alice", Block: "b1", View: 1},
		{Voter: "bob", Block: "b1", View: 1},
		{Voter: "carol", Block: "b1", View: 1},
	}

	var want QC
	for replica := range committee.Members() {
		collector := NewVoteSet(committee, "b1", 1)
		for _, vote := range votes {
			require.NoError(t, collector.Add(vote))
		}

		got, ok := collector.QC()
		require.Truef(t, ok, "replica %d did not form a QC", replica)
		if replica == 0 {
			want = got
		}
		require.Equal(t, want, got)
	}
}

func TestVoteSetRejectsDuplicateVoter(t *testing.T) {
	committee, err := NewCommittee([]Member{
		{ID: "alice", Power: 2},
		{ID: "bob", Power: 1},
		{ID: "carol", Power: 1},
	})
	require.NoError(t, err)

	collector := NewVoteSet(committee, "b1", 1)
	vote := Vote{Voter: "alice", Block: "b1", View: 1}
	require.NoError(t, collector.Add(vote))
	require.ErrorIs(t, collector.Add(vote), ErrDuplicateVote)

	_, ok := collector.QC()
	require.False(t, ok, "a duplicate vote must not increase voting power")
}

func TestVoteSetSerializesConcurrentDuplicateVoter(t *testing.T) {
	committee, err := NewCommittee([]Member{
		{ID: "alice", Power: 1},
		{ID: "bob", Power: 1},
		{ID: "carol", Power: 1},
	})
	require.NoError(t, err)
	collector := NewVoteSet(committee, "b1", 1)

	const attempts = 32
	start := make(chan struct{})
	results := make(chan error, attempts)
	var ready sync.WaitGroup
	ready.Add(attempts)
	for range attempts {
		go func() {
			ready.Done()
			<-start
			results <- collector.Add(Vote{Voter: "alice", Block: "b1", View: 1})
		}()
	}
	ready.Wait()
	close(start)

	successes := 0
	for range attempts {
		if err := <-results; err == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	_, formed := collector.QC()
	require.False(t, formed)
}

func TestDirectThreeChainCommitsGreatGrandparent(t *testing.T) {
	core := NewCore(Block{ID: "genesis", View: 0})

	chain := []Block{
		{ID: "b1", Parent: "genesis", View: 1, Justify: QC{Block: "genesis", View: 0}},
		{ID: "b2", Parent: "b1", View: 2, Justify: QC{Block: "b1", View: 1}},
		{ID: "b3", Parent: "b2", View: 3, Justify: QC{Block: "b2", View: 2}},
		{ID: "b4", Parent: "b3", View: 4, Justify: QC{Block: "b3", View: 3}},
	}

	for _, block := range chain[:3] {
		committed, err := core.Accept(block)
		require.NoError(t, err)
		require.Empty(t, committed)
	}

	committed, err := core.Accept(chain[3])
	require.NoError(t, err)
	require.Equal(t, []BlockID{"b1"}, committed)
	require.Equal(t, BlockID("b1"), core.Committed())
}

func TestCoreRejectsProposalWhoseQCDoesNotJustifyParent(t *testing.T) {
	core := NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, acceptWithoutCommit(core,
		Block{ID: "b1", Parent: "genesis", View: 1, Justify: QC{Block: "genesis", View: 0}},
	))

	_, err := core.Accept(Block{
		ID:      "bad",
		Parent:  "b1",
		View:    2,
		Justify: QC{Block: "genesis", View: 0},
	})
	require.ErrorIs(t, err, ErrQCDoesNotJustifyParent)
}

func acceptWithoutCommit(core *Core, block Block) error {
	_, err := core.Accept(block)
	return err
}
