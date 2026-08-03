package harmony

import (
	"errors"

	"github.com/harmony-one/harmony/consensus/hotstuff"
	hmybls "github.com/harmony-one/harmony/crypto/bls"
	"github.com/harmony-one/harmony/shard"
)

// ErrDuplicateBLSKeyOwner indicates that one BLS key is assigned to different validators.
var ErrDuplicateBLSKeyOwner = errors.New("hotstuff Harmony committee BLS key has multiple validator owners")

// ValidatorSchedule derives a validator-level leader order from Harmony's
// BLS-slot committee. Multiple BLS keys owned by one ECDSA address occupy one
// position in the schedule.
type ValidatorSchedule struct {
	// committee is used only for canonical member order and leader rotation.
	// Its uniform power must not be used as Harmony quorum voting power.
	committee *hotstuff.Committee
	keyOwners map[hmybls.SerializedPublicKey]hotstuff.MemberID
}

// NewValidatorSchedule groups Harmony BLS slots by ECDSA validator identity in
// first-slot order. The source must be canonical chain committee state.
func NewValidatorSchedule(source *shard.Committee) (*ValidatorSchedule, error) {
	if source == nil {
		return nil, shard.ErrSubCommitteeNil
	}

	members := make([]hotstuff.Member, 0, len(source.Slots))
	keyOwners := make(map[hmybls.SerializedPublicKey]hotstuff.MemberID, len(source.Slots))
	seenValidators := make(map[hotstuff.MemberID]struct{}, len(source.Slots))
	for _, slot := range source.Slots {
		memberID := hotstuff.MemberID(slot.EcdsaAddress.Hex())
		if owner, exists := keyOwners[slot.BLSPublicKey]; exists {
			if owner != memberID {
				return nil, ErrDuplicateBLSKeyOwner
			}
			return nil, hotstuff.ErrDuplicateBLSPublicKey
		}
		keyOwners[slot.BLSPublicKey] = memberID
		if _, exists := seenValidators[memberID]; exists {
			continue
		}
		seenValidators[memberID] = struct{}{}
		members = append(members, hotstuff.Member{ID: memberID, Power: 1})
	}

	committee, err := hotstuff.NewCommittee(members)
	if err != nil {
		return nil, err
	}
	return &ValidatorSchedule{committee: committee, keyOwners: keyOwners}, nil
}

// Members returns validator identities in canonical leader order.
func (s *ValidatorSchedule) Members() []hotstuff.MemberID {
	members := s.committee.Members()
	ids := make([]hotstuff.MemberID, len(members))
	for i, member := range members {
		ids[i] = member.ID
	}
	return ids
}

// Leader returns the validator assigned to the given view.
func (s *ValidatorSchedule) Leader(view hotstuff.View) hotstuff.MemberID {
	return s.committee.Leader(view)
}

// ValidatorForKey returns the validator that owns a Harmony BLS slot key.
func (s *ValidatorSchedule) ValidatorForKey(key hmybls.SerializedPublicKey) (hotstuff.MemberID, bool) {
	memberID, exists := s.keyOwners[key]
	return memberID, exists
}
