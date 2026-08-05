package harmony

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/harmony-one/harmony/api/proto"
	wirepb "github.com/harmony-one/harmony/api/proto/hotstuff"
	"github.com/harmony-one/harmony/consensus/hotstuff"
	"github.com/harmony-one/harmony/p2p"
	"github.com/stretchr/testify/require"
	protobuf "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestHotStuffWireVoteRoundTripUsesDedicatedCategory(t *testing.T) {
	domain := hotstuff.VoteDomain{
		ChainID: 1666700000,
		ShardID: 2,
		Epoch:   42,
		Genesis: testWireBlockID(0x01),
	}
	vote := hotstuff.SignedVote{
		Vote: hotstuff.Vote{
			Voter: testWireMemberID(0x02),
			Block: testWireBlockID(0x03),
			View:  17,
		},
		Signature: bytes.Repeat([]byte{0x5a}, 96),
	}

	encoded, err := EncodeVoteMessage(domain, vote)
	require.NoError(t, err)
	category, err := proto.GetMessageCategory(encoded)
	require.NoError(t, err)
	require.Equal(t, proto.HotStuff, category)

	decoded, err := DecodeWireMessage(encoded)
	require.NoError(t, err)
	require.Equal(t, WireVersion, decoded.Version)
	require.Equal(t, domain, decoded.Domain)
	require.NotNil(t, decoded.Vote)
	require.Equal(t, vote, *decoded.Vote)

	encoded[len(encoded)-1] ^= 0xff
	require.Equal(t, bytes.Repeat([]byte{0x5a}, 96), decoded.Vote.Signature)
}

func TestHotStuffWireProposalRoundTripOwnsBlockAndQC(t *testing.T) {
	domain := hotstuff.VoteDomain{
		ChainID: 1666700000,
		ShardID: 2,
		Epoch:   42,
		Genesis: testWireBlockID(0x01),
	}
	block := []byte{0xf8, 0x01, 0x02, 0x03}
	justify := hotstuff.BLSQC{
		QC: hotstuff.QC{
			Block: testWireBlockID(0x04),
			View:  16,
			Signers: []hotstuff.MemberID{
				testWireMemberID(0x05),
				testWireMemberID(0x06),
			},
		},
		Signature: bytes.Repeat([]byte{0x6b}, 96),
		Bitmap:    []byte{0x03},
	}

	encoded, err := EncodeProposalMessage(domain, block, justify)
	require.NoError(t, err)
	decoded, err := DecodeWireMessage(encoded)
	require.NoError(t, err)
	require.Nil(t, decoded.Vote)
	require.NotNil(t, decoded.Proposal)
	require.Equal(t, block, decoded.Proposal.Block)
	require.Equal(t, justify, decoded.Proposal.Justify)

	for index := range encoded {
		encoded[index] ^= 0xff
	}
	require.Equal(t, []byte{0xf8, 0x01, 0x02, 0x03}, decoded.Proposal.Block)
	require.Equal(t, bytes.Repeat([]byte{0x6b}, 96), decoded.Proposal.Justify.Signature)
	require.Equal(t, []byte{0x03}, decoded.Proposal.Justify.Bitmap)
}

func TestHotStuffWireProposalAcceptsOnlyCanonicalGenesisTrustRoot(t *testing.T) {
	domain := hotstuff.VoteDomain{
		ChainID: 1666700000,
		ShardID: 2,
		Epoch:   42,
		Genesis: testWireBlockID(0x01),
	}
	block := []byte{0xf8, 0x01, 0x02, 0x03}
	genesis := hotstuff.BLSQC{
		QC: hotstuff.QC{Block: domain.Genesis},
	}

	encoded, err := EncodeProposalMessage(domain, block, genesis)
	require.NoError(t, err)
	decoded, err := DecodeWireMessageForDomain(encoded, domain)
	require.NoError(t, err)
	require.NotNil(t, decoded.Proposal)
	require.Equal(t, genesis, decoded.Proposal.Justify)
	foreignDomain := domain
	foreignDomain.Epoch++
	_, err = DecodeWireMessageForDomain(encoded, foreignDomain)
	require.ErrorIs(t, err, ErrWireDomainMismatch)

	invalidEncode := map[string]hotstuff.BLSQC{
		"wrong block": {
			QC: hotstuff.QC{Block: testWireBlockID(0x02)},
		},
		"nonzero view": {
			QC: hotstuff.QC{Block: domain.Genesis, View: 1},
		},
		"partial signers": {
			QC: hotstuff.QC{Block: domain.Genesis, Signers: []hotstuff.MemberID{testWireMemberID(0x03)}},
		},
		"partial signature": {
			QC:        hotstuff.QC{Block: domain.Genesis},
			Signature: bytes.Repeat([]byte{0x44}, 96),
		},
		"partial bitmap": {
			QC:     hotstuff.QC{Block: domain.Genesis},
			Bitmap: []byte{0x01},
		},
	}
	for name, justify := range invalidEncode {
		t.Run("encode "+name, func(t *testing.T) {
			_, err := EncodeProposalMessage(domain, block, justify)
			require.ErrorIs(t, err, ErrInvalidWireMessage)
		})
	}

	invalidDecode := map[string]func(*wirepb.QuorumCertificate){
		"wrong block": func(qc *wirepb.QuorumCertificate) {
			qc.Block = []byte(testWireBlockID(0x02))
		},
		"nonzero view": func(qc *wirepb.QuorumCertificate) {
			qc.View = 1
		},
		"partial signers": func(qc *wirepb.QuorumCertificate) {
			qc.Signers = []byte(testWireMemberID(0x03))
		},
		"partial signature": func(qc *wirepb.QuorumCertificate) {
			qc.Signature = bytes.Repeat([]byte{0x44}, 96)
		},
		"partial bitmap": func(qc *wirepb.QuorumCertificate) {
			qc.Bitmap = []byte{0x01}
		},
	}
	for name, mutateQC := range invalidDecode {
		t.Run("decode "+name, func(t *testing.T) {
			malformed := mutateWireEnvelope(t, encoded, func(envelope *wirepb.Envelope) {
				mutateQC(envelope.GetProposal().Justify)
			})
			_, err := DecodeWireMessageForDomain(malformed, domain)
			require.ErrorIs(t, err, ErrInvalidWireMessage)
		})
	}
}

func TestHotStuffWireDecodeRejectsEveryDomainMismatch(t *testing.T) {
	domain := hotstuff.VoteDomain{
		ChainID: 1666700000,
		ShardID: 2,
		Epoch:   42,
		Genesis: testWireBlockID(0x01),
	}
	vote := hotstuff.SignedVote{
		Vote: hotstuff.Vote{
			Voter: testWireMemberID(0x02),
			Block: testWireBlockID(0x03),
			View:  17,
		},
		Signature: bytes.Repeat([]byte{0x5a}, 96),
	}
	encoded, err := EncodeVoteMessage(domain, vote)
	require.NoError(t, err)

	tests := map[string]hotstuff.VoteDomain{
		"chain":   {ChainID: domain.ChainID + 1, ShardID: domain.ShardID, Epoch: domain.Epoch, Genesis: domain.Genesis},
		"shard":   {ChainID: domain.ChainID, ShardID: domain.ShardID + 1, Epoch: domain.Epoch, Genesis: domain.Genesis},
		"epoch":   {ChainID: domain.ChainID, ShardID: domain.ShardID, Epoch: domain.Epoch + 1, Genesis: domain.Genesis},
		"genesis": {ChainID: domain.ChainID, ShardID: domain.ShardID, Epoch: domain.Epoch, Genesis: testWireBlockID(0x07)},
	}
	for name, expected := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeWireMessageForDomain(encoded, expected)
			require.ErrorIs(t, err, ErrWireDomainMismatch)
		})
	}

	decoded, err := DecodeWireMessageForDomain(encoded, domain)
	require.NoError(t, err)
	require.Equal(t, vote, *decoded.Vote)
}

func TestHotStuffWireEncodeRejectsNonCanonicalIdentities(t *testing.T) {
	domain := hotstuff.VoteDomain{
		ChainID: 1666700000,
		ShardID: 2,
		Epoch:   42,
		Genesis: "0xgenesis",
	}
	vote := hotstuff.SignedVote{
		Vote: hotstuff.Vote{
			Voter: "bls:validator-key",
			Block: "0xblock",
			View:  17,
		},
		Signature: bytes.Repeat([]byte{0x5a}, 96),
	}

	_, err := EncodeVoteMessage(domain, vote)
	require.ErrorIs(t, err, ErrInvalidWireMessage)
}

func TestHotStuffWireDecodeRejectsMalformedEnvelopeBeforeDispatch(t *testing.T) {
	domain := hotstuff.VoteDomain{
		ChainID: 1666700000,
		ShardID: 2,
		Epoch:   42,
		Genesis: testWireBlockID(0x01),
	}
	vote := hotstuff.SignedVote{
		Vote: hotstuff.Vote{
			Voter: testWireMemberID(0x02),
			Block: testWireBlockID(0x03),
			View:  17,
		},
		Signature: bytes.Repeat([]byte{0x5a}, 96),
	}
	valid, err := EncodeVoteMessage(domain, vote)
	require.NoError(t, err)

	tests := map[string]struct {
		message []byte
		target  error
	}{
		"wrong category": {
			message: append([]byte{byte(proto.Node)}, valid[proto.MessageCategoryBytes:]...),
			target:  ErrWrongWireCategory,
		},
		"unsupported version": {
			message: mutateWireEnvelope(t, valid, func(envelope *wirepb.Envelope) {
				envelope.Version++
			}),
			target: ErrUnsupportedWireVersion,
		},
		"missing domain": {
			message: mutateWireEnvelope(t, valid, func(envelope *wirepb.Envelope) {
				envelope.Domain = nil
			}),
			target: ErrInvalidWireMessage,
		},
		"missing body": {
			message: mutateWireEnvelope(t, valid, func(envelope *wirepb.Envelope) {
				envelope.Message = nil
			}),
			target: ErrInvalidWireMessage,
		},
		"short signature": {
			message: mutateWireEnvelope(t, valid, func(envelope *wirepb.Envelope) {
				envelope.GetVote().Signature = envelope.GetVote().Signature[:95]
			}),
			target: ErrInvalidWireMessage,
		},
		"alternate uppercase voter": {
			message: mutateWireEnvelope(t, valid, func(envelope *wirepb.Envelope) {
				envelope.GetVote().Voter[len("bls:")] = 'A'
			}),
			target: ErrInvalidWireMessage,
		},
		"oversized frame": {
			message: append([]byte{byte(proto.HotStuff)}, make([]byte, MaxWireMessageSize)...),
			target:  ErrWireMessageTooLarge,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeWireMessage(test.message)
			require.ErrorIs(t, err, test.target)
		})
	}
}

func TestHotStuffWireQCSignersUseBoundedPackedEncoding(t *testing.T) {
	fields := (&wirepb.QuorumCertificate{}).ProtoReflect().Descriptor().Fields()
	signers := fields.ByName("signers")
	require.NotNil(t, signers)
	require.Equal(t, protoreflect.Optional, signers.Cardinality())
	require.Equal(t, protoreflect.BytesKind, signers.Kind())
}

func TestHotStuffWireMaximumProposalFitsP2PFrame(t *testing.T) {
	domain := hotstuff.VoteDomain{
		ChainID: 1,
		ShardID: 2,
		Epoch:   3,
		Genesis: testWireBlockID(1),
	}
	signers := make([]hotstuff.MemberID, maxWireSigners)
	for index := range signers {
		signers[index] = testWireMemberID(byte(index))
	}
	encoded, err := EncodeProposalMessage(
		domain,
		make([]byte, MaxWireBlockSize),
		hotstuff.BLSQC{
			QC: hotstuff.QC{
				Block:   testWireBlockID(2),
				View:    4,
				Signers: signers,
			},
			Signature: bytes.Repeat([]byte{0x44}, 96),
			Bitmap:    bytes.Repeat([]byte{0xff}, maxWireBitmapSize),
		},
	)
	require.NoError(t, err)
	require.LessOrEqual(t, len(p2p.ConstructMessage(encoded)), p2p.MaxMessageSize)
}

func mutateWireEnvelope(
	t *testing.T,
	message []byte,
	mutate func(*wirepb.Envelope),
) []byte {
	t.Helper()
	envelope := &wirepb.Envelope{}
	require.NoError(t, protobuf.Unmarshal(message[proto.MessageCategoryBytes:], envelope))
	mutate(envelope)
	payload, err := protobuf.Marshal(envelope)
	require.NoError(t, err)
	return proto.ConstructHotStuffMessage(payload)
}

func testWireBlockID(fill byte) hotstuff.BlockID {
	return hotstuff.BlockID("0x" + hex.EncodeToString(bytes.Repeat([]byte{fill}, 32)))
}

func testWireMemberID(fill byte) hotstuff.MemberID {
	return hotstuff.MemberID("bls:" + hex.EncodeToString(bytes.Repeat([]byte{fill}, 48)))
}
