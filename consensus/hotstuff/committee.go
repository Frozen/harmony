package hotstuff

import (
	"errors"
	"math"
)

var (
	ErrEmptyCommittee      = errors.New("hotstuff committee is empty")
	ErrInvalidMember       = errors.New("hotstuff committee member is invalid")
	ErrDuplicateMember     = errors.New("hotstuff committee member is duplicated")
	ErrVotingPowerOverflow = errors.New("hotstuff committee voting power overflows uint64")
)

// Member is a validator identity and its integer voting power.
type Member struct {
	ID    MemberID
	Power uint64
}

// Committee is an ordered, weighted validator set. Its order defines the
// deterministic round-robin leader schedule.
type Committee struct {
	members []Member
	byID    map[MemberID]Member
	total   uint64
}

func NewCommittee(members []Member) (*Committee, error) {
	if len(members) == 0 {
		return nil, ErrEmptyCommittee
	}

	committee := &Committee{
		members: append([]Member(nil), members...),
		byID:    make(map[MemberID]Member, len(members)),
	}
	for _, member := range members {
		if member.ID == "" || member.Power == 0 {
			return nil, ErrInvalidMember
		}
		if _, exists := committee.byID[member.ID]; exists {
			return nil, ErrDuplicateMember
		}
		if committee.total > math.MaxUint64-member.Power {
			return nil, ErrVotingPowerOverflow
		}
		committee.byID[member.ID] = member
		committee.total += member.Power
	}
	return committee, nil
}

func (c *Committee) Members() []Member {
	return append([]Member(nil), c.members...)
}

// Leader rotates on every view. View one starts with the first committee
// member; view zero is reserved for genesis.
func (c *Committee) Leader(view View) MemberID {
	if view == 0 {
		return c.members[0].ID
	}
	return c.members[(uint64(view)-1)%uint64(len(c.members))].ID
}

func (c *Committee) quorumPower() uint64 {
	// The smallest integer voting power strictly greater than two thirds.
	return c.total - (c.total-1)/3
}
