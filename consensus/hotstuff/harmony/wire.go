package harmony

import (
	"errors"

	hmyproto "github.com/harmony-one/harmony/api/proto"
	wirepb "github.com/harmony-one/harmony/api/proto/hotstuff"
	"github.com/harmony-one/harmony/consensus/hotstuff"
	hmybls "github.com/harmony-one/harmony/crypto/bls"
	"google.golang.org/protobuf/proto"
)

const (
	// WireVersion identifies the first dedicated Harmony HotStuff envelope.
	WireVersion uint32 = 1
	// maxP2PFrameSize matches p2p.MaxMessageSize. ConstructMessage adds the
	// five-byte outer frame before the dedicated HotStuff category.
	maxP2PFrameSize           = 1 << 21
	p2pOuterMessagePrefixSize = 5
	maxWireSigners            = 4096
	wireMemberIDSize          = len("bls:") + (hmybls.PublicKeySizeInBytes << 1)
	maxWireBitmapSize         = (maxWireSigners + 7) >> 3
	maxWireEnvelopeOverhead   = maxWireSigners*wireMemberIDSize + maxWireBitmapSize + (8 << 10)
	// MaxWireMessageSize bounds decode work before protobuf unmarshalling and
	// keeps the completed outer frame within the host transport limit.
	MaxWireMessageSize = maxP2PFrameSize - p2pOuterMessagePrefixSize
	// MaxWireBlockSize reserves enough of that frame for the largest bounded QC.
	MaxWireBlockSize = MaxWireMessageSize - maxWireEnvelopeOverhead
)

var (
	ErrWireMessageTooLarge    = errors.New("hotstuff wire message exceeds the size limit")
	ErrWrongWireCategory      = errors.New("hotstuff wire message has the wrong category")
	ErrUnsupportedWireVersion = errors.New("hotstuff wire message version is unsupported")
	ErrWireDomainMismatch     = errors.New("hotstuff wire message belongs to a different domain")
	ErrInvalidWireMessage     = errors.New("hotstuff wire message is invalid")
)

// WireMessage is an owned, decoded HotStuff transport envelope.
type WireMessage struct {
	Version  uint32
	Domain   hotstuff.VoteDomain
	Vote     *hotstuff.SignedVote
	Proposal *WireProposal
}

// WireProposal contains an owned Harmony block encoding and untrusted QC
// evidence. Signed evidence must pass QCAuthority.Verify; an empty genesis
// trust root must match the capability minted while configuring that authority.
type WireProposal struct {
	Block   []byte
	Justify hotstuff.BLSQC
}

// EncodeVoteMessage serializes one signed HotStuff vote in the dedicated
// versioned HotStuff category. It never uses the legacy FBFT ConsensusRequest.
func EncodeVoteMessage(domain hotstuff.VoteDomain, vote hotstuff.SignedVote) ([]byte, error) {
	if err := validateWireBlockID(string(domain.Genesis)); err != nil {
		return nil, err
	}
	if err := validateWireMemberID(string(vote.Vote.Voter)); err != nil {
		return nil, err
	}
	if err := validateWireBlockID(string(vote.Vote.Block)); err != nil {
		return nil, err
	}
	if len(vote.Signature) != hmybls.BLSSignatureSizeInBytes {
		return nil, ErrInvalidWireMessage
	}
	envelope := &wirepb.Envelope{
		Version: WireVersion,
		Domain: &wirepb.Domain{
			ChainId: domain.ChainID,
			ShardId: domain.ShardID,
			Epoch:   domain.Epoch,
			Genesis: []byte(domain.Genesis),
		},
		Message: &wirepb.Envelope_Vote{
			Vote: &wirepb.Vote{
				Voter:     []byte(vote.Vote.Voter),
				Block:     []byte(vote.Vote.Block),
				View:      uint64(vote.Vote.View),
				Signature: append([]byte(nil), vote.Signature...),
			},
		},
	}
	payload, err := proto.Marshal(envelope)
	if err != nil {
		return nil, ErrInvalidWireMessage
	}
	message := hmyproto.ConstructHotStuffMessage(payload)
	if len(message) > MaxWireMessageSize {
		return nil, ErrWireMessageTooLarge
	}
	return message, nil
}

// EncodeProposalMessage serializes one Harmony block and its parent QC
// evidence. Before core ingress, the receiver must decode the block and either
// verify signed evidence or match the configured genesis trust-root capability.
func EncodeProposalMessage(
	domain hotstuff.VoteDomain,
	block []byte,
	justify hotstuff.BLSQC,
) ([]byte, error) {
	if err := validateWireBlockID(string(domain.Genesis)); err != nil {
		return nil, err
	}
	if len(block) == 0 || len(block) > MaxWireBlockSize {
		return nil, ErrInvalidWireMessage
	}
	wireQC, err := encodeWireQC(domain, justify)
	if err != nil {
		return nil, err
	}
	envelope := &wirepb.Envelope{
		Version: WireVersion,
		Domain: &wirepb.Domain{
			ChainId: domain.ChainID,
			ShardId: domain.ShardID,
			Epoch:   domain.Epoch,
			Genesis: []byte(domain.Genesis),
		},
		Message: &wirepb.Envelope_Proposal{
			Proposal: &wirepb.Proposal{
				Block:   append([]byte(nil), block...),
				Justify: wireQC,
			},
		},
	}
	payload, err := proto.Marshal(envelope)
	if err != nil {
		return nil, ErrInvalidWireMessage
	}
	message := hmyproto.ConstructHotStuffMessage(payload)
	if len(message) > MaxWireMessageSize {
		return nil, ErrWireMessageTooLarge
	}
	return message, nil
}

// DecodeWireMessage validates and owns one dedicated HotStuff transport
// envelope before it can reach certificate or state-machine processing.
func DecodeWireMessage(message []byte) (WireMessage, error) {
	if len(message) > MaxWireMessageSize {
		return WireMessage{}, ErrWireMessageTooLarge
	}
	category, err := hmyproto.GetMessageCategory(message)
	if err != nil || category != hmyproto.HotStuff {
		return WireMessage{}, ErrWrongWireCategory
	}
	if len(message) == hmyproto.MessageCategoryBytes {
		return WireMessage{}, ErrInvalidWireMessage
	}
	envelope := &wirepb.Envelope{}
	if err := proto.Unmarshal(message[hmyproto.MessageCategoryBytes:], envelope); err != nil {
		return WireMessage{}, ErrInvalidWireMessage
	}
	if envelope.Version != WireVersion {
		return WireMessage{}, ErrUnsupportedWireVersion
	}
	if envelope.Domain == nil {
		return WireMessage{}, ErrInvalidWireMessage
	}
	if err := validateWireBlockBytes(envelope.Domain.Genesis); err != nil {
		return WireMessage{}, err
	}
	result := WireMessage{
		Version: envelope.Version,
		Domain: hotstuff.VoteDomain{
			ChainID: envelope.Domain.ChainId,
			ShardID: envelope.Domain.ShardId,
			Epoch:   envelope.Domain.Epoch,
			Genesis: hotstuff.BlockID(string(envelope.Domain.Genesis)),
		},
	}
	if wireVote := envelope.GetVote(); wireVote != nil {
		vote, err := decodeWireVote(wireVote)
		if err != nil {
			return WireMessage{}, err
		}
		result.Vote = vote
		return result, nil
	}
	if wireProposal := envelope.GetProposal(); wireProposal != nil {
		proposal, err := decodeWireProposal(result.Domain, wireProposal)
		if err != nil {
			return WireMessage{}, err
		}
		result.Proposal = proposal
		return result, nil
	}
	return WireMessage{}, ErrInvalidWireMessage
}

// DecodeWireMessageForDomain rejects cross-chain, cross-shard, cross-epoch,
// and cross-genesis replay before returning a message to network ingress.
func DecodeWireMessageForDomain(
	message []byte,
	expected hotstuff.VoteDomain,
) (WireMessage, error) {
	decoded, err := DecodeWireMessage(message)
	if err != nil {
		return WireMessage{}, err
	}
	if decoded.Domain != expected {
		return WireMessage{}, ErrWireDomainMismatch
	}
	return decoded, nil
}

func encodeWireQC(domain hotstuff.VoteDomain, qc hotstuff.BLSQC) (*wirepb.QuorumCertificate, error) {
	if err := validateWireBlockID(string(qc.QC.Block)); err != nil {
		return nil, err
	}
	if isGenesisTrustRootQC(
		domain,
		string(qc.QC.Block),
		uint64(qc.QC.View),
		len(qc.QC.Signers),
		len(qc.Signature),
		len(qc.Bitmap),
	) {
		return &wirepb.QuorumCertificate{
			Block: []byte(qc.QC.Block),
			View:  uint64(qc.QC.View),
		}, nil
	}
	if len(qc.QC.Signers) == 0 || len(qc.QC.Signers) > maxWireSigners ||
		len(qc.Signature) != hmybls.BLSSignatureSizeInBytes ||
		len(qc.Bitmap) == 0 || len(qc.Bitmap) > maxWireBitmapSize {
		return nil, ErrInvalidWireMessage
	}
	signers := make([]byte, 0, len(qc.QC.Signers)*wireMemberIDSize)
	for _, signer := range qc.QC.Signers {
		if err := validateWireMemberID(string(signer)); err != nil {
			return nil, err
		}
		signers = append(signers, []byte(signer)...)
	}
	return &wirepb.QuorumCertificate{
		Block:     []byte(qc.QC.Block),
		View:      uint64(qc.QC.View),
		Signers:   signers,
		Signature: append([]byte(nil), qc.Signature...),
		Bitmap:    append([]byte(nil), qc.Bitmap...),
	}, nil
}

func decodeWireVote(wireVote *wirepb.Vote) (*hotstuff.SignedVote, error) {
	if err := validateWireMemberBytes(wireVote.Voter); err != nil {
		return nil, err
	}
	if err := validateWireBlockBytes(wireVote.Block); err != nil {
		return nil, err
	}
	if len(wireVote.Signature) != hmybls.BLSSignatureSizeInBytes {
		return nil, ErrInvalidWireMessage
	}
	return &hotstuff.SignedVote{
		Vote: hotstuff.Vote{
			Voter: hotstuff.MemberID(string(wireVote.Voter)),
			Block: hotstuff.BlockID(string(wireVote.Block)),
			View:  hotstuff.View(wireVote.View),
		},
		Signature: append([]byte(nil), wireVote.Signature...),
	}, nil
}

func decodeWireProposal(domain hotstuff.VoteDomain, wireProposal *wirepb.Proposal) (*WireProposal, error) {
	if len(wireProposal.Block) == 0 || len(wireProposal.Block) > MaxWireBlockSize || wireProposal.Justify == nil {
		return nil, ErrInvalidWireMessage
	}
	wireQC := wireProposal.Justify
	if err := validateWireBlockBytes(wireQC.Block); err != nil {
		return nil, err
	}
	if !isGenesisTrustRootQC(
		domain,
		string(wireQC.Block),
		wireQC.View,
		len(wireQC.Signers),
		len(wireQC.Signature),
		len(wireQC.Bitmap),
	) && (len(wireQC.Signers) == 0 || len(wireQC.Signers)%wireMemberIDSize != 0 ||
		len(wireQC.Signers) > maxWireSigners*wireMemberIDSize ||
		len(wireQC.Signature) != hmybls.BLSSignatureSizeInBytes ||
		len(wireQC.Bitmap) == 0 || len(wireQC.Bitmap) > maxWireBitmapSize) {
		return nil, ErrInvalidWireMessage
	}
	var signers []hotstuff.MemberID
	if len(wireQC.Signers) > 0 {
		signers = make([]hotstuff.MemberID, len(wireQC.Signers)/wireMemberIDSize)
	}
	for index := range signers {
		start := index * wireMemberIDSize
		signer := wireQC.Signers[start : start+wireMemberIDSize]
		if err := validateWireMemberBytes(signer); err != nil {
			return nil, err
		}
		signers[index] = hotstuff.MemberID(string(signer))
	}
	return &WireProposal{
		Block: append([]byte(nil), wireProposal.Block...),
		Justify: hotstuff.BLSQC{
			QC: hotstuff.QC{
				Block:   hotstuff.BlockID(string(wireQC.Block)),
				View:    hotstuff.View(wireQC.View),
				Signers: signers,
			},
			Signature: append([]byte(nil), wireQC.Signature...),
			Bitmap:    append([]byte(nil), wireQC.Bitmap...),
		},
	}, nil
}

func isGenesisTrustRootQC(
	domain hotstuff.VoteDomain,
	block string,
	view uint64,
	signerEvidenceSize int,
	signatureSize int,
	bitmapSize int,
) bool {
	return hotstuff.BlockID(block) == domain.Genesis &&
		view == 0 &&
		signerEvidenceSize == 0 &&
		signatureSize == 0 &&
		bitmapSize == 0
}

func validateWireBlockID(id string) error {
	return validateWireHexID(id, "0x", 32)
}

func validateWireMemberID(id string) error {
	return validateWireHexID(id, "bls:", hmybls.PublicKeySizeInBytes)
}

func validateWireBlockBytes(id []byte) error {
	return validateWireBlockID(string(id))
}

func validateWireMemberBytes(id []byte) error {
	return validateWireMemberID(string(id))
}

func validateWireHexID(id, prefix string, byteLength int) error {
	if len(id) != len(prefix)+(byteLength<<1) || id[:len(prefix)] != prefix {
		return ErrInvalidWireMessage
	}
	for _, character := range id[len(prefix):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return ErrInvalidWireMessage
		}
	}
	return nil
}
