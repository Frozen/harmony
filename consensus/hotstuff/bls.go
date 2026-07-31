package hotstuff

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"sync"

	hmybls "github.com/harmony-one/harmony/crypto/bls"
	bls_core "github.com/harmony-one/harmony/crypto/bls/core"
)

const hotStuffVoteDomain = "harmony/hotstuff/vote/v1"

var (
	ErrDuplicateBLSPublicKey = errors.New("hotstuff committee contains a duplicate BLS public key")
	ErrInvalidBLSPublicKey   = errors.New("hotstuff committee contains an invalid BLS public key")
	ErrInvalidVoteSignature  = errors.New("hotstuff vote has an invalid BLS signature")
	ErrInvalidQCSignature    = errors.New("hotstuff QC has an invalid aggregate BLS signature")
	ErrInvalidQCBitmap       = errors.New("hotstuff QC bitmap does not match its signers")
	ErrNonCanonicalQCSigners = errors.New("hotstuff QC signers are not in canonical committee order")
	ErrNilBLSSecretKey       = errors.New("hotstuff cannot sign with a nil BLS secret key")
	ErrBlockIDTooLong        = errors.New("hotstuff block ID is too long to sign")
)

// VoteDomain prevents a HotStuff vote from being replayed across chains,
// shards, epochs, or other consensus message types.
type VoteDomain struct {
	ChainID uint32
	ShardID uint32
	Epoch   uint64
}

// BLSMember must contain a key already admitted by Harmony's validator-key
// registry after proof-of-possession validation.
type BLSMember struct {
	Member
	PublicKey hmybls.PublicKeyWrapper
}

// BLSCommittee binds the structural voting-power committee to Harmony BLS
// public keys in the same canonical member order.
type BLSCommittee struct {
	committee *Committee
	members   []BLSMember
	byID      map[MemberID]BLSMember
	publics   []hmybls.PublicKeyWrapper
}

// NewBLSCommitteeFromValidatedKeys constructs a committee from keys that have
// already passed staking/types.VerifyBLSKey (or an equivalent registry check).
// It must not be called directly on public keys supplied by a network peer.
func NewBLSCommitteeFromValidatedKeys(members []BLSMember) (*BLSCommittee, error) {
	structuralMembers := make([]Member, 0, len(members))
	for _, member := range members {
		structuralMembers = append(structuralMembers, member.Member)
	}
	committee, err := NewCommittee(structuralMembers)
	if err != nil {
		return nil, err
	}

	result := &BLSCommittee{
		committee: committee,
		members:   make([]BLSMember, 0, len(members)),
		byID:      make(map[MemberID]BLSMember, len(members)),
		publics:   make([]hmybls.PublicKeyWrapper, 0, len(members)),
	}
	seenKeys := make(map[hmybls.SerializedPublicKey]struct{}, len(members))
	for _, member := range members {
		if member.PublicKey.Bytes.IsEmpty() {
			return nil, ErrInvalidBLSPublicKey
		}
		if _, exists := seenKeys[member.PublicKey.Bytes]; exists {
			return nil, ErrDuplicateBLSPublicKey
		}

		public := &bls_core.PublicKey{}
		if err := public.Deserialize(member.PublicKey.Bytes.Bytes()); err != nil {
			return nil, ErrInvalidBLSPublicKey
		}
		owned := BLSMember{
			Member: member.Member,
			PublicKey: hmybls.PublicKeyWrapper{
				Bytes:  member.PublicKey.Bytes,
				Object: public,
			},
		}
		seenKeys[owned.PublicKey.Bytes] = struct{}{}
		result.members = append(result.members, owned)
		result.byID[owned.ID] = owned
		result.publics = append(result.publics, owned.PublicKey)
	}
	return result, nil
}

type SignedVote struct {
	Vote      Vote
	Signature []byte
}

type BLSQC struct {
	QC        QC
	Signature []byte
	Bitmap    []byte
}

func SignVote(domain VoteDomain, vote Vote, secret *bls_core.SecretKey) (SignedVote, error) {
	if secret == nil {
		return SignedVote{}, ErrNilBLSSecretKey
	}
	digest, err := voteDigest(domain, vote)
	if err != nil {
		return SignedVote{}, err
	}
	signature := secret.SignHash(digest[:])
	if signature == nil {
		return SignedVote{}, ErrInvalidVoteSignature
	}
	return SignedVote{
		Vote:      vote,
		Signature: append([]byte(nil), signature.Serialize()...),
	}, nil
}

// BLSVoteSet verifies individual broadcast votes before passing their voting
// power to the structural collector and aggregates a QC at quorum.
type BLSVoteSet struct {
	mu         sync.Mutex
	committee  *BLSCommittee
	votes      *VoteSet
	domain     VoteDomain
	signatures map[MemberID]*bls_core.Sign
}

func NewBLSVoteSet(committee *BLSCommittee, block BlockID, view View, domain VoteDomain) *BLSVoteSet {
	return &BLSVoteSet{
		committee:  committee,
		votes:      NewVoteSet(committee.committee, block, view),
		domain:     domain,
		signatures: make(map[MemberID]*bls_core.Sign),
	}
}

func (s *BLSVoteSet) Add(vote SignedVote) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	member, exists := s.committee.byID[vote.Vote.Voter]
	if !exists {
		return ErrUnknownVoter
	}
	signature, err := deserializeSignature(vote.Signature, ErrInvalidVoteSignature)
	if err != nil {
		return err
	}
	digest, err := voteDigest(s.domain, vote.Vote)
	if err != nil {
		return err
	}
	if !signature.VerifyHash(member.PublicKey.Object, digest[:]) {
		return ErrInvalidVoteSignature
	}
	if err := s.votes.Add(vote.Vote); err != nil {
		return err
	}
	s.signatures[vote.Vote.Voter] = signature
	return nil
}

func (s *BLSVoteSet) QC() (BLSQC, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	qc, formed := s.votes.QC()
	if !formed {
		return BLSQC{}, false
	}
	signatures := make([]*bls_core.Sign, 0, len(qc.Signers))
	mask := hmybls.NewMask(s.committee.publics)
	for _, signer := range qc.Signers {
		signatures = append(signatures, s.signatures[signer])
		member := s.committee.byID[signer]
		if err := mask.SetKey(member.PublicKey.Bytes, true); err != nil {
			return BLSQC{}, false
		}
	}
	aggregate := hmybls.AggregateSig(signatures)
	return BLSQC{
		QC:        qc,
		Signature: append([]byte(nil), aggregate.Serialize()...),
		Bitmap:    mask.Mask(),
	}, true
}

func (c *BLSCommittee) VerifyQC(domain VoteDomain, qc BLSQC) error {
	if err := c.committee.requireQC(qc.QC); err != nil {
		return err
	}
	canonical := c.committee.canonicalQC(qc.QC)
	if !equalMemberIDs(qc.QC.Signers, canonical.Signers) {
		return ErrNonCanonicalQCSigners
	}
	mask := hmybls.NewMask(c.publics)
	for _, signer := range qc.QC.Signers {
		member := c.byID[signer]
		if err := mask.SetKey(member.PublicKey.Bytes, true); err != nil {
			return ErrInvalidQCBitmap
		}
	}
	if !bytes.Equal(mask.Mask(), qc.Bitmap) {
		return ErrInvalidQCBitmap
	}
	signature, err := deserializeSignature(qc.Signature, ErrInvalidQCSignature)
	if err != nil {
		return err
	}
	digest, err := voteDigest(domain, Vote{Block: qc.QC.Block, View: qc.QC.View})
	if err != nil {
		return err
	}
	if !signature.VerifyHash(mask.AggregatePublic, digest[:]) {
		return ErrInvalidQCSignature
	}
	return nil
}

func equalMemberIDs(left, right []MemberID) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func deserializeSignature(serialized []byte, invalid error) (*bls_core.Sign, error) {
	if len(serialized) != hmybls.BLSSignatureSizeInBytes {
		return nil, invalid
	}
	signature := &bls_core.Sign{}
	if err := signature.Deserialize(append([]byte(nil), serialized...)); err != nil {
		return nil, invalid
	}
	return signature, nil
}

func voteDigest(domain VoteDomain, vote Vote) ([sha256.Size]byte, error) {
	if uint64(len(vote.Block)) > uint64(math.MaxUint32) {
		return [sha256.Size]byte{}, ErrBlockIDTooLong
	}
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(hotStuffVoteDomain))
	var fixed [8]byte
	binary.BigEndian.PutUint32(fixed[:4], domain.ChainID)
	_, _ = hasher.Write(fixed[:4])
	binary.BigEndian.PutUint32(fixed[:4], domain.ShardID)
	_, _ = hasher.Write(fixed[:4])
	binary.BigEndian.PutUint64(fixed[:], domain.Epoch)
	_, _ = hasher.Write(fixed[:])
	binary.BigEndian.PutUint64(fixed[:], uint64(vote.View))
	_, _ = hasher.Write(fixed[:])
	binary.BigEndian.PutUint32(fixed[:4], uint32(len(vote.Block)))
	_, _ = hasher.Write(fixed[:4])
	_, _ = hasher.Write([]byte(vote.Block))

	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest, nil
}
