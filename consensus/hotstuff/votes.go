package hotstuff

import (
	"errors"
	"sync"
)

var (
	ErrUnknownVoter  = errors.New("hotstuff vote is from an unknown committee member")
	ErrWrongVote     = errors.New("hotstuff vote targets a different block or view")
	ErrDuplicateVote = errors.New("hotstuff voter already voted for this block and view")
)

// VoteSet collects broadcast votes for one proposal. Every replica may build
// the same QC; a later transport adapter may instead collect only at the next
// leader without changing this type.
type VoteSet struct {
	mu        sync.Mutex
	committee *Committee
	block     BlockID
	view      View
	voters    map[MemberID]struct{}
	power     uint64
}

func NewVoteSet(committee *Committee, block BlockID, view View) *VoteSet {
	return &VoteSet{
		committee: committee,
		block:     block,
		view:      view,
		voters:    make(map[MemberID]struct{}),
	}
}

func (s *VoteSet) Add(vote Vote) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	member, exists := s.committee.byID[vote.Voter]
	if !exists {
		return ErrUnknownVoter
	}
	if vote.Block != s.block || vote.View != s.view {
		return ErrWrongVote
	}
	if _, exists := s.voters[vote.Voter]; exists {
		return ErrDuplicateVote
	}

	s.voters[vote.Voter] = struct{}{}
	s.power += member.Power
	return nil
}

func (s *VoteSet) QC() (QC, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.power < s.committee.quorumPower() {
		return QC{}, false
	}

	signers := make([]MemberID, 0, len(s.voters))
	for _, member := range s.committee.members {
		if _, voted := s.voters[member.ID]; voted {
			signers = append(signers, member.ID)
		}
	}
	return QC{Block: s.block, View: s.view, Signers: signers}, true
}
