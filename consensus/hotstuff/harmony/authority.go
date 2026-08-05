package harmony

import (
	"errors"
	"math/big"

	"github.com/harmony-one/harmony/consensus/hotstuff"
	hmybls "github.com/harmony-one/harmony/crypto/bls"
	"github.com/harmony-one/harmony/shard"
)

// ErrQuorumDomainMismatch indicates that the authority vote domain does not
// identify the shard committee epoch used to derive its quorum roster.
var ErrQuorumDomainMismatch = errors.New("hotstuff Harmony quorum domain does not match committee epoch or shard")

// NewStakingQCAuthority binds one canonical staking-era Harmony committee to
// validator-level leader rotation and exact BLS-slot QC/TC quorum semantics.
// Activation callers must still gate this constructor with
// ChainConfig.IsStaking(epoch).
func NewStakingQCAuthority(
	source *shard.Committee,
	epoch *big.Int,
	domain hotstuff.VoteDomain,
) (*hotstuff.QCAuthority, error) {
	owned := cloneAuthorityCommittee(source)
	quorum, err := NewHarmonyQuorum(owned, epoch)
	if err != nil {
		return nil, err
	}
	if epoch.BitLen() > 64 || domain.Epoch != epoch.Uint64() || domain.ShardID != owned.ShardID {
		return nil, ErrQuorumDomainMismatch
	}
	leaders, err := NewValidatorSchedule(owned)
	if err != nil {
		return nil, err
	}

	slots := quorum.Slots()
	members := make([]hotstuff.BLSMember, len(slots))
	for index, slot := range slots {
		members[index] = hotstuff.BLSMember{
			Member: hotstuff.Member{ID: slot.ID, Power: 1},
			PublicKey: hmybls.PublicKeyWrapper{
				Bytes: slot.PublicKey,
			},
		}
	}
	committee, err := hotstuff.NewBLSCommitteeFromValidatedKeysWithQuorum(members, quorum)
	if err != nil {
		return nil, err
	}
	return hotstuff.NewQCAuthorityWithLeaderSchedule(committee, domain, leaders)
}

func cloneAuthorityCommittee(source *shard.Committee) *shard.Committee {
	if source == nil {
		return nil
	}
	owned := &shard.Committee{
		ShardID: source.ShardID,
		Slots:   make(shard.SlotList, len(source.Slots)),
	}
	copy(owned.Slots, source.Slots)
	return owned
}
