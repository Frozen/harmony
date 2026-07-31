package hotstuff

import (
	"errors"
	"sync"
)

var (
	ErrAlreadyVoted     = errors.New("hotstuff replica already voted in this or a higher view")
	ErrUnsafeProposal   = errors.New("hotstuff proposal neither extends the lock nor carries a higher QC")
	ErrProposalMismatch = errors.New("hotstuff proposal does not match the accepted block")
	ErrMissingPersister = errors.New("hotstuff safety state persister is missing")
)

// SafetyState is the minimum state that must survive a validator restart.
// Persisting it before emitting a vote prevents crash-recovery double voting.
type SafetyState struct {
	LastVotedView View
	LockedQC      QC
}

// PersistSafetyState must durably store the complete state before returning.
type PersistSafetyState func(SafetyState) error

// SafetyRules implements HotStuff's last-voted and locked-QC voting rules.
// It expects proposals to pass Core.Accept before Vote is called.
type SafetyRules struct {
	mu      sync.Mutex
	core    *Core
	state   SafetyState
	persist PersistSafetyState
}

func NewSafetyRules(core *Core, initial SafetyState, persist PersistSafetyState) *SafetyRules {
	initial = cloneSafetyState(initial)
	return &SafetyRules{core: core, state: initial, persist: persist}
}

func (r *SafetyRules) State() SafetyState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneSafetyState(r.state)
}

// Vote applies the safe-node rule and durably advances safety state before it
// returns a vote that may be broadcast.
func (r *SafetyRules) Vote(voter MemberID, proposal Block) (Vote, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	accepted, exists := r.core.block(proposal.ID)
	if !exists {
		return Vote{}, ErrUnknownParent
	}
	if !sameProposal(accepted, proposal) {
		return Vote{}, ErrProposalMismatch
	}
	if proposal.View <= r.state.LastVotedView {
		return Vote{}, ErrAlreadyVoted
	}
	if !r.core.Extends(proposal.ID, r.state.LockedQC.Block) &&
		proposal.Justify.View <= r.state.LockedQC.View {
		return Vote{}, ErrUnsafeProposal
	}
	if r.persist == nil {
		return Vote{}, ErrMissingPersister
	}

	next := cloneSafetyState(r.state)
	next.LastVotedView = proposal.View
	if lock, ok := r.core.lockQC(proposal); ok && lock.View > next.LockedQC.View {
		next.LockedQC = lock
	}
	if err := r.persist(cloneSafetyState(next)); err != nil {
		return Vote{}, err
	}
	r.state = next

	return Vote{Voter: voter, Block: proposal.ID, View: proposal.View}, nil
}

func sameProposal(left, right Block) bool {
	return left.ID == right.ID &&
		left.Parent == right.Parent &&
		left.View == right.View &&
		left.Justify.Block == right.Justify.Block &&
		left.Justify.View == right.Justify.View
}

func cloneQC(qc QC) QC {
	qc.Signers = append([]MemberID(nil), qc.Signers...)
	return qc
}

func cloneSafetyState(state SafetyState) SafetyState {
	state.LockedQC = cloneQC(state.LockedQC)
	return state
}
