package hotstuff

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSafetyRulesRejectsSecondVoteInSameView(t *testing.T) {
	core := newCore(Block{ID: "genesis", View: 0})
	persist := &recordingPersister{}
	rules := NewSafetyRules(core, SafetyState{
		LockedQC: QC{Block: "genesis", View: 0},
	}, persist.Save)

	b1 := Block{ID: "b1", Parent: "genesis", View: 1, Justify: QC{Block: "genesis", View: 0}}
	require.NoError(t, acceptWithoutCommit(core, b1))
	_, err := rules.Vote("alice", b1)
	require.NoError(t, err)

	fork := Block{ID: "fork", Parent: "genesis", View: 1, Justify: QC{Block: "genesis", View: 0}}
	require.NoError(t, acceptWithoutCommit(core, fork))
	_, err = rules.Vote("alice", fork)
	require.ErrorIs(t, err, ErrAlreadyVoted)
	require.Len(t, persist.states, 1)
}

func TestSafetyRulesVotesForDescendantOfLockedBlock(t *testing.T) {
	core := newCore(Block{ID: "genesis", View: 0})
	b1 := Block{ID: "b1", Parent: "genesis", View: 1, Justify: QC{Block: "genesis", View: 0}}
	b2 := Block{ID: "b2", Parent: "b1", View: 2, Justify: QC{Block: "b1", View: 1}}
	require.NoError(t, acceptWithoutCommit(core, b1))
	require.NoError(t, acceptWithoutCommit(core, b2))

	persist := &recordingPersister{}
	rules := NewSafetyRules(core, SafetyState{
		LastVotedView: 2,
		LockedQC:      QC{Block: "genesis", View: 0},
	}, persist.Save)

	b3 := Block{ID: "b3", Parent: "b2", View: 3, Justify: QC{Block: "b2", View: 2}}
	require.NoError(t, acceptWithoutCommit(core, b3))
	vote, err := rules.Vote("alice", b3)
	require.NoError(t, err)
	require.Equal(t, Vote{Voter: "alice", Block: "b3", View: 3}, vote)
	require.Equal(t, View(3), rules.State().LastVotedView)
	require.Equal(t, QC{Block: "b1", View: 1}, rules.State().LockedQC)
	require.Equal(t, rules.State(), persist.states[0])
}

func TestSafetyRulesUnlocksForHigherQCOnConflictingBranch(t *testing.T) {
	core := newCore(Block{ID: "genesis", View: 0})
	locked := Block{ID: "locked", Parent: "genesis", View: 1, Justify: QC{Block: "genesis", View: 0}}
	fork1 := Block{ID: "fork1", Parent: "genesis", View: 2, Justify: QC{Block: "genesis", View: 0}}
	fork2 := Block{ID: "fork2", Parent: "fork1", View: 3, Justify: QC{Block: "fork1", View: 2}}
	for _, block := range []Block{locked, fork1, fork2} {
		require.NoError(t, acceptWithoutCommit(core, block))
	}

	persist := &recordingPersister{}
	rules := NewSafetyRules(core, SafetyState{
		LastVotedView: 1,
		LockedQC:      QC{Block: "locked", View: 1},
	}, persist.Save)

	vote, err := rules.Vote("alice", fork2)
	require.NoError(t, err)
	require.Equal(t, Vote{Voter: "alice", Block: "fork2", View: 3}, vote)
}

func TestSafetyRulesRejectsConflictingBranchWithoutHigherQC(t *testing.T) {
	core := newCore(Block{ID: "genesis", View: 0})
	locked := Block{ID: "locked", Parent: "genesis", View: 2, Justify: QC{Block: "genesis", View: 0}}
	fork := Block{ID: "fork", Parent: "genesis", View: 3, Justify: QC{Block: "genesis", View: 0}}
	for _, block := range []Block{locked, fork} {
		require.NoError(t, acceptWithoutCommit(core, block))
	}

	persist := &recordingPersister{}
	rules := NewSafetyRules(core, SafetyState{
		LastVotedView: 2,
		LockedQC:      QC{Block: "locked", View: 2},
	}, persist.Save)

	_, err := rules.Vote("alice", fork)
	require.ErrorIs(t, err, ErrUnsafeProposal)
	require.Empty(t, persist.states)
}

func TestSafetyRulesRejectsConflictingBranchWithEqualQC(t *testing.T) {
	core := newCore(Block{ID: "genesis", View: 0})
	locked := Block{ID: "locked", Parent: "genesis", View: 2, Justify: QC{Block: "genesis", View: 0}}
	fork := Block{ID: "fork", Parent: "genesis", View: 2, Justify: QC{Block: "genesis", View: 0}}
	proposal := Block{ID: "proposal", Parent: "fork", View: 3, Justify: QC{Block: "fork", View: 2}}
	for _, block := range []Block{locked, fork, proposal} {
		require.NoError(t, acceptWithoutCommit(core, block))
	}

	rules := NewSafetyRules(core, SafetyState{
		LastVotedView: 2,
		LockedQC:      QC{Block: "locked", View: 2},
	}, func(SafetyState) error { return nil })

	_, err := rules.Vote("alice", proposal)
	require.ErrorIs(t, err, ErrUnsafeProposal)
}

func TestSafetyRulesRestoresLastVoteAcrossRestart(t *testing.T) {
	core := newCore(Block{ID: "genesis", View: 0})
	left := Block{ID: "left", Parent: "genesis", View: 1, Justify: QC{Block: "genesis", View: 0}}
	right := Block{ID: "right", Parent: "genesis", View: 1, Justify: QC{Block: "genesis", View: 0}}
	require.NoError(t, acceptWithoutCommit(core, left))
	require.NoError(t, acceptWithoutCommit(core, right))

	var persisted SafetyState
	rules := NewSafetyRules(core, SafetyState{
		LockedQC: QC{Block: "genesis", View: 0},
	}, func(state SafetyState) error {
		persisted = state
		return nil
	})
	_, err := rules.Vote("alice", left)
	require.NoError(t, err)

	restarted := NewSafetyRules(core, persisted, func(SafetyState) error { return nil })
	_, err = restarted.Vote("alice", right)
	require.ErrorIs(t, err, ErrAlreadyVoted)
}

func TestSafetyRulesRetainsHigherLock(t *testing.T) {
	core := newCore(Block{ID: "genesis", View: 0})
	low := Block{ID: "low", Parent: "genesis", View: 1, Justify: QC{Block: "genesis", View: 0}}
	locked := Block{ID: "locked", Parent: "genesis", View: 5, Justify: QC{Block: "genesis", View: 0}}
	parent := Block{ID: "parent", Parent: "low", View: 6, Justify: QC{Block: "low", View: 1}}
	proposal := Block{ID: "proposal", Parent: "parent", View: 7, Justify: QC{Block: "parent", View: 6}}
	for _, block := range []Block{low, locked, parent, proposal} {
		require.NoError(t, acceptWithoutCommit(core, block))
	}

	rules := NewSafetyRules(core, SafetyState{
		LastVotedView: 5,
		LockedQC:      QC{Block: "locked", View: 5},
	}, func(SafetyState) error { return nil })

	_, err := rules.Vote("alice", proposal)
	require.NoError(t, err)
	require.Equal(t, QC{Block: "locked", View: 5}, rules.State().LockedQC)
}

func TestSafetyRulesPersistsBeforeReturningVote(t *testing.T) {
	core := newCore(Block{ID: "genesis", View: 0})
	b1 := Block{ID: "b1", Parent: "genesis", View: 1, Justify: QC{Block: "genesis", View: 0}}
	require.NoError(t, acceptWithoutCommit(core, b1))

	persistErr := errors.New("disk unavailable")
	rules := NewSafetyRules(core, SafetyState{
		LockedQC: QC{Block: "genesis", View: 0},
	}, func(SafetyState) error {
		return persistErr
	})

	_, err := rules.Vote("alice", b1)
	require.ErrorIs(t, err, persistErr)
	require.Equal(t, View(0), rules.State().LastVotedView)
}

func TestSafetyRulesDoesNotShareStateWithPersister(t *testing.T) {
	core := newCore(Block{ID: "genesis", View: 0})
	b1 := Block{
		ID:      "b1",
		Parent:  "genesis",
		View:    1,
		Justify: QC{Block: "genesis", View: 0, Signers: []MemberID{"alice"}},
	}
	b2 := Block{
		ID:      "b2",
		Parent:  "b1",
		View:    2,
		Justify: QC{Block: "b1", View: 1, Signers: []MemberID{"alice"}},
	}
	b3 := Block{
		ID:      "b3",
		Parent:  "b2",
		View:    3,
		Justify: QC{Block: "b2", View: 2, Signers: []MemberID{"alice"}},
	}
	for _, block := range []Block{b1, b2, b3} {
		require.NoError(t, acceptWithoutCommit(core, block))
	}

	rules := NewSafetyRules(core, SafetyState{
		LockedQC: QC{Block: "genesis", View: 0},
	}, func(state SafetyState) error {
		state.LockedQC.Signers[0] = "mallory"
		return nil
	})

	_, err := rules.Vote("alice", b3)
	require.NoError(t, err)
	require.Equal(t, []MemberID{"alice"}, rules.State().LockedQC.Signers)
}

func TestSafetyRulesSerializesConflictingVotes(t *testing.T) {
	core := newCore(Block{ID: "genesis", View: 0})
	left := Block{ID: "left", Parent: "genesis", View: 1, Justify: QC{Block: "genesis", View: 0}}
	right := Block{ID: "right", Parent: "genesis", View: 1, Justify: QC{Block: "genesis", View: 0}}
	require.NoError(t, acceptWithoutCommit(core, left))
	require.NoError(t, acceptWithoutCommit(core, right))

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	rules := NewSafetyRules(core, SafetyState{
		LockedQC: QC{Block: "genesis", View: 0},
	}, func(SafetyState) error {
		entered <- struct{}{}
		<-release
		return nil
	})

	results := make(chan error, 2)
	var started sync.WaitGroup
	started.Add(2)
	for _, proposal := range []Block{left, right} {
		go func(proposal Block) {
			started.Done()
			started.Wait()
			_, err := rules.Vote("alice", proposal)
			results <- err
		}(proposal)
	}

	<-entered
	select {
	case <-entered:
		close(release)
	case <-time.After(50 * time.Millisecond):
		close(release)
	}

	successes := 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes, "two conflicting votes escaped in one view")
}

func TestCoreOwnsAcceptedQCEvidence(t *testing.T) {
	core := newCore(Block{ID: "genesis", View: 0})
	b1 := Block{ID: "b1", Parent: "genesis", View: 1, Justify: QC{Block: "genesis", View: 0}}
	b2 := Block{
		ID:      "b2",
		Parent:  "b1",
		View:    2,
		Justify: QC{Block: "b1", View: 1, Signers: []MemberID{"alice"}},
	}
	require.NoError(t, acceptWithoutCommit(core, b1))
	require.NoError(t, acceptWithoutCommit(core, b2))
	b2.Justify.Signers[0] = "mallory"

	b3 := Block{ID: "b3", Parent: "b2", View: 3, Justify: QC{Block: "b2", View: 2}}
	require.NoError(t, acceptWithoutCommit(core, b3))
	rules := NewSafetyRules(core, SafetyState{
		LockedQC: QC{Block: "genesis", View: 0},
	}, func(SafetyState) error { return nil })

	_, err := rules.Vote("alice", b3)
	require.NoError(t, err)
	require.Equal(t, []MemberID{"alice"}, rules.State().LockedQC.Signers)
}

type recordingPersister struct {
	states []SafetyState
}

func (p *recordingPersister) Save(state SafetyState) error {
	p.states = append(p.states, state)
	return nil
}
