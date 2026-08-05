package node

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"

	"github.com/harmony-one/harmony/consensus/hotstuff"
	hshotstuff "github.com/harmony-one/harmony/consensus/hotstuff/harmony"
	"github.com/stretchr/testify/require"
)

type testHotStuffShadowHandler struct {
	domain  hotstuff.VoteDomain
	handled []hshotstuff.WireMessage
}

func (handler *testHotStuffShadowHandler) Domain() hotstuff.VoteDomain {
	return handler.domain
}

func (handler *testHotStuffShadowHandler) Handle(
	_ context.Context,
	message hshotstuff.WireMessage,
) error {
	handler.handled = append(handler.handled, message)
	return nil
}

func TestHotStuffShadowIngressIsDisabledByDefault(t *testing.T) {
	node := &Node{}
	message := testHotStuffVoteMessage(t, testHotStuffDomain())

	_, _, err := node.validateHotStuffShadowMessage(message)
	require.ErrorIs(t, err, ErrHotStuffShadowDisabled)
}

func TestHotStuffShadowIngressBindsDomainBeforeDispatch(t *testing.T) {
	node := &Node{}
	domain := testHotStuffDomain()
	handler := &testHotStuffShadowHandler{domain: domain}
	require.NoError(t, node.SetHotStuffShadowHandler(handler))

	message := testHotStuffVoteMessage(t, domain)
	target, decoded, err := node.validateHotStuffShadowMessage(message)
	require.NoError(t, err)
	require.Same(t, handler, target)
	require.NoError(t, target.Handle(context.Background(), decoded))
	require.Len(t, handler.handled, 1)

	wrongDomain := domain
	wrongDomain.Epoch++
	message = testHotStuffVoteMessage(t, wrongDomain)
	_, _, err = node.validateHotStuffShadowMessage(message)
	require.ErrorIs(t, err, hshotstuff.ErrWireDomainMismatch)
	require.Len(t, handler.handled, 1)
}

func TestHotStuffShadowIngressRejectsTypedNilHandler(t *testing.T) {
	node := &Node{}
	var handler *testHotStuffShadowHandler
	require.ErrorIs(t, node.SetHotStuffShadowHandler(handler), ErrInvalidHotStuffShadowHandler)
}

func TestHotStuffShadowIngressRejectsNonConsensusTopic(t *testing.T) {
	node := &Node{}
	domain := testHotStuffDomain()
	require.NoError(t, node.SetHotStuffShadowHandler(&testHotStuffShadowHandler{domain: domain}))

	message := testHotStuffVoteMessage(t, domain)
	_, _, err := node.validateHotStuffShadowTopicMessage(message, false)
	require.ErrorIs(t, err, errConsensusMessageOnUnexpectedTopic)
}

func TestHotStuffShadowIngressSnapshotsDomainAtRegistration(t *testing.T) {
	node := &Node{}
	domain := testHotStuffDomain()
	handler := &testHotStuffShadowHandler{domain: domain}
	require.NoError(t, node.SetHotStuffShadowHandler(handler))

	handler.domain.Epoch++
	target, decoded, err := node.validateHotStuffShadowMessage(testHotStuffVoteMessage(t, domain))
	require.NoError(t, err)
	require.Same(t, handler, target)
	require.Equal(t, domain, decoded.Domain)
}

func TestHotStuffShadowIngressCanBeDisabledExplicitly(t *testing.T) {
	node := &Node{}
	domain := testHotStuffDomain()
	require.NoError(t, node.SetHotStuffShadowHandler(&testHotStuffShadowHandler{domain: domain}))
	require.NoError(t, node.SetHotStuffShadowHandler(nil))

	_, _, err := node.validateHotStuffShadowMessage(testHotStuffVoteMessage(t, domain))
	require.ErrorIs(t, err, ErrHotStuffShadowDisabled)
}

func testHotStuffDomain() hotstuff.VoteDomain {
	return hotstuff.VoteDomain{
		ChainID: 1666700000,
		ShardID: 2,
		Epoch:   42,
		Genesis: testHotStuffBlockID(0x01),
	}
}

func testHotStuffVoteMessage(t *testing.T, domain hotstuff.VoteDomain) []byte {
	t.Helper()
	message, err := hshotstuff.EncodeVoteMessage(domain, hotstuff.SignedVote{
		Vote: hotstuff.Vote{
			Voter: testHotStuffMemberID(0x02),
			Block: testHotStuffBlockID(0x03),
			View:  17,
		},
		Signature: bytes.Repeat([]byte{0x5a}, 96),
	})
	require.NoError(t, err)
	return message
}

func testHotStuffBlockID(fill byte) hotstuff.BlockID {
	return hotstuff.BlockID("0x" + hex.EncodeToString(bytes.Repeat([]byte{fill}, 32)))
}

func testHotStuffMemberID(fill byte) hotstuff.MemberID {
	return hotstuff.MemberID("bls:" + hex.EncodeToString(bytes.Repeat([]byte{fill}, 48)))
}
