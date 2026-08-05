package harmony

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/harmony-one/harmony/consensus/hotstuff"
	"github.com/harmony-one/harmony/consensus/votepower"
	hmybls "github.com/harmony-one/harmony/crypto/bls"
	"github.com/harmony-one/harmony/numeric"
	"github.com/harmony-one/harmony/shard"
)

var (
	ErrNilQuorumEpoch           = errors.New("hotstuff Harmony quorum epoch is nil")
	ErrInvalidQuorumEpoch       = errors.New("hotstuff Harmony quorum epoch is negative")
	ErrInvalidQuorumVotingPower = errors.New("hotstuff Harmony quorum has invalid voting power")
	ErrInvalidQuorumBitmap      = errors.New("hotstuff Harmony quorum bitmap has invalid length")
	ErrNonCanonicalQuorumBitmap = errors.New("hotstuff Harmony quorum bitmap sets unused committee bits")
	ErrMissingQuorumVotingPower = errors.New("hotstuff Harmony quorum slot is missing voting power")
)

var harmonyQuorumThreshold = numeric.NewDec(2).Quo(numeric.NewDec(3))

// HarmonyQuorumSlot is one canonical Harmony BLS voting identity. Validator is
// informational ownership metadata; every BLS slot retains its own bitmap
// position and exact EPoS voting power.
type HarmonyQuorumSlot struct {
	ID          hotstuff.MemberID
	Validator   common.Address
	PublicKey   hmybls.SerializedPublicKey
	VotingPower numeric.Dec
}

// HarmonyQuorum is an isolated staking-era reference adapter for Harmony's
// canonical BLS-slot roster and decimal EPoS voting power. It does not yet
// govern the HotStuff core's QC or timeout-certificate paths, and it must not
// reuse the validator-level leader schedule's uniform structural power.
type HarmonyQuorum struct {
	slots []HarmonyQuorumSlot
	byID  map[hotstuff.MemberID]int
}

// NewHarmonyQuorum derives an immutable staking-era slot-level quorum roster
// from canonical chain committee state, never peer-supplied keys. Its voting
// powers match consensus/votepower.Compute.
func NewHarmonyQuorum(source *shard.Committee, epoch *big.Int) (*HarmonyQuorum, error) {
	if source == nil {
		return nil, shard.ErrSubCommitteeNil
	}
	if epoch == nil {
		return nil, ErrNilQuorumEpoch
	}
	if epoch.Sign() < 0 {
		return nil, ErrInvalidQuorumEpoch
	}
	if len(source.Slots) == 0 {
		return nil, hotstuff.ErrEmptyCommittee
	}

	owned := &shard.Committee{
		ShardID: source.ShardID,
		Slots:   make(shard.SlotList, len(source.Slots)),
	}
	keyOwners := make(map[hmybls.SerializedPublicKey]hotstuff.MemberID, len(source.Slots))
	for index, slot := range source.Slots {
		if slot.BLSPublicKey.IsEmpty() {
			return nil, fmt.Errorf("slot %d: %w", index, hotstuff.ErrInvalidBLSPublicKey)
		}
		if _, err := hmybls.BytesToBLSPublicKey(slot.BLSPublicKey.Bytes()); err != nil {
			return nil, fmt.Errorf("slot %d: %w", index, hotstuff.ErrInvalidBLSPublicKey)
		}
		if owner, exists := keyOwners[slot.BLSPublicKey]; exists {
			if owner != hotstuff.MemberID(slot.EcdsaAddress.Hex()) {
				return nil, ErrDuplicateBLSKeyOwner
			}
			return nil, hotstuff.ErrDuplicateBLSPublicKey
		}
		keyOwners[slot.BLSPublicKey] = hotstuff.MemberID(slot.EcdsaAddress.Hex())

		owned.Slots[index] = slot
		if slot.EffectiveStake != nil {
			if slot.EffectiveStake.IsNil() || !slot.EffectiveStake.IsPositive() {
				return nil, fmt.Errorf("slot %d: %w", index, ErrInvalidQuorumVotingPower)
			}
			stake := slot.EffectiveStake.Copy()
			owned.Slots[index].EffectiveStake = &stake
		}
	}

	roster, err := votepower.Compute(owned, new(big.Int).Set(epoch))
	if err != nil {
		return nil, fmt.Errorf("compute Harmony quorum voting power: %w", err)
	}

	result := &HarmonyQuorum{
		slots: make([]HarmonyQuorumSlot, 0, len(owned.Slots)),
		byID:  make(map[hotstuff.MemberID]int, len(owned.Slots)),
	}
	total := numeric.ZeroDec()
	for index, slot := range owned.Slots {
		voter, exists := roster.Voters[slot.BLSPublicKey]
		if !exists || voter == nil || voter.OverallPercent.IsNil() {
			return nil, fmt.Errorf("slot %d: %w", index, ErrMissingQuorumVotingPower)
		}
		if voter.OverallPercent.IsNegative() {
			return nil, fmt.Errorf("slot %d: %w", index, ErrInvalidQuorumVotingPower)
		}
		memberID := HarmonyQuorumMemberID(slot.BLSPublicKey)
		result.byID[memberID] = index
		power := voter.OverallPercent.Copy()
		result.slots = append(result.slots, HarmonyQuorumSlot{
			ID:          memberID,
			Validator:   slot.EcdsaAddress,
			PublicKey:   slot.BLSPublicKey,
			VotingPower: power,
		})
		total = total.Add(power)
	}
	if !total.Equal(numeric.OneDec()) {
		return nil, fmt.Errorf("total %s: %w", total.String(), ErrInvalidQuorumVotingPower)
	}
	return result, nil
}

// HarmonyQuorumMemberID returns the stable HotStuff signer identity for one
// Harmony BLS committee slot.
func HarmonyQuorumMemberID(key hmybls.SerializedPublicKey) hotstuff.MemberID {
	return hotstuff.MemberID("bls:" + key.Hex())
}

// Slots returns the canonical BLS-slot roster with owned decimal values.
func (q *HarmonyQuorum) Slots() []HarmonyQuorumSlot {
	slots := make([]HarmonyQuorumSlot, len(q.slots))
	for index, slot := range q.slots {
		slots[index] = slot
		slots[index].VotingPower = slot.VotingPower.Copy()
	}
	return slots
}

// Threshold returns Harmony's strict two-thirds comparison threshold.
func (q *HarmonyQuorum) Threshold() numeric.Dec {
	return harmonyQuorumThreshold.Copy()
}

// Bitmap returns the canonical little-endian committee bitmap for signers.
func (q *HarmonyQuorum) Bitmap(signers []hotstuff.MemberID) ([]byte, error) {
	bitmap := make([]byte, q.bitmapLength())
	seen := make(map[hotstuff.MemberID]struct{}, len(signers))
	for _, signer := range signers {
		if _, exists := seen[signer]; exists {
			return nil, hotstuff.ErrDuplicateSigner
		}
		index, exists := q.byID[signer]
		if !exists {
			return nil, hotstuff.ErrUnknownVoter
		}
		seen[signer] = struct{}{}
		bitmap[index>>3] |= byte(1) << uint(index&7)
	}
	return bitmap, nil
}

// Signers decodes a canonical bitmap into canonical BLS-slot order.
func (q *HarmonyQuorum) Signers(bitmap []byte) ([]hotstuff.MemberID, error) {
	if err := q.validateBitmap(bitmap); err != nil {
		return nil, err
	}
	signers := make([]hotstuff.MemberID, 0, len(q.slots))
	for index, slot := range q.slots {
		if bitmap[index>>3]&(byte(1)<<uint(index&7)) != 0 {
			signers = append(signers, slot.ID)
		}
	}
	return signers, nil
}

// VotingPower returns the exact decimal sum represented by a canonical bitmap.
func (q *HarmonyQuorum) VotingPower(bitmap []byte) (numeric.Dec, error) {
	if err := q.validateBitmap(bitmap); err != nil {
		return numeric.Dec{}, err
	}
	power := numeric.ZeroDec()
	for index, slot := range q.slots {
		if bitmap[index>>3]&(byte(1)<<uint(index&7)) != 0 {
			power = power.Add(slot.VotingPower)
		}
	}
	return power, nil
}

// IsQuorum reports whether the bitmap has voting power strictly greater than
// two thirds, matching Harmony's staked quorum rule.
func (q *HarmonyQuorum) IsQuorum(bitmap []byte) (bool, error) {
	power, err := q.VotingPower(bitmap)
	if err != nil {
		return false, err
	}
	return power.GT(harmonyQuorumThreshold), nil
}

func (q *HarmonyQuorum) bitmapLength() int {
	return (len(q.slots) + 7) >> 3
}

func (q *HarmonyQuorum) validateBitmap(bitmap []byte) error {
	if len(bitmap) != q.bitmapLength() {
		return ErrInvalidQuorumBitmap
	}
	if remainder := len(q.slots) & 7; remainder != 0 {
		unused := ^byte((1 << uint(remainder)) - 1)
		if bitmap[len(bitmap)-1]&unused != 0 {
			return ErrNonCanonicalQuorumBitmap
		}
	}
	return nil
}
