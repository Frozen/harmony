package consensus

import (
	"math/big"
	"testing"
	"time"

	"github.com/harmony-one/harmony/crypto/bls"
	"github.com/harmony-one/harmony/internal/params"
	"github.com/harmony-one/harmony/internal/registry"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	msg_pb "github.com/harmony-one/harmony/api/proto/message"
	"github.com/harmony-one/harmony/consensus/quorum"
	"github.com/harmony-one/harmony/internal/utils"
	"github.com/harmony-one/harmony/multibls"
	"github.com/harmony-one/harmony/p2p"
	"github.com/harmony-one/harmony/shard"
)

func TestSignAndMarshalConsensusMessage(t *testing.T) {
	leader := p2p.Peer{IP: "127.0.0.1", Port: "9902"}
	priKey, _, _ := utils.GenKeyP2P("127.0.0.1", "9902")
	host, err := p2p.NewHost(p2p.HostConfig{
		Self:   &leader,
		BLSKey: priKey,
	})
	if err != nil {
		t.Fatalf("newhost failure: %v", err)
	}
	decider := quorum.NewDecider(quorum.SuperMajorityVote, shard.BeaconChainShardID)
	blsPriKey := bls.RandPrivateKey()
	reg := registry.New()
	consensus, err := New(host, shard.BeaconChainShardID, multibls.GetPrivateKeys(blsPriKey), reg, decider, 3, false)
	if err != nil {
		t.Fatalf("Cannot craeate consensus: %v", err)
	}
	consensus.SetCurBlockViewID(2)
	consensus.current.blockHash = [32]byte{}

	msg := &msg_pb.Message{}
	marshaledMessage, err := consensus.signAndMarshalConsensusMessage(msg, blsPriKey)

	if err != nil || len(marshaledMessage) == 0 {
		t.Errorf("Failed to sign and marshal the message: %s", err)
	}
	if len(msg.Signature) == 0 {
		t.Error("No signature is signed on the consensus message.")
	}
}

func TestSetViewID(t *testing.T) {
	leader := p2p.Peer{IP: "127.0.0.1", Port: "9902"}
	priKey, _, _ := utils.GenKeyP2P("127.0.0.1", "9902")
	host, err := p2p.NewHost(p2p.HostConfig{
		Self:   &leader,
		BLSKey: priKey,
	})
	if err != nil {
		t.Fatalf("newhost failure: %v", err)
	}
	decider := quorum.NewDecider(
		quorum.SuperMajorityVote, shard.BeaconChainShardID,
	)
	blsPriKey := bls.RandPrivateKey()
	reg := registry.New()
	consensus, err := New(
		host, shard.BeaconChainShardID, multibls.GetPrivateKeys(blsPriKey), reg, decider, 3, false,
	)
	if err != nil {
		t.Fatalf("Cannot craeate consensus: %v", err)
	}

	height := uint64(1000)
	consensus.SetViewIDs(height)
	if consensus.GetCurBlockViewID() != height {
		t.Errorf("Cannot set consensus ID. Got: %v, Expected: %v", consensus.GetCurBlockViewID(), height)
	}
}

func TestErrors(t *testing.T) {
	e1 := errors.New("e1")
	require.True(t, errors.Is(e1, e1))

	t.Run("wrap", func(t *testing.T) {
		e2 := errors.Wrap(e1, "e2")
		require.True(t, errors.Is(e2, e1))
	})

	t.Run("withMessage", func(t *testing.T) {
		e2 := errors.WithMessage(e1, "e2")
		require.True(t, errors.Is(e2, e1))
	})
}

func TestBlockPeriodForConsensusUpdate(t *testing.T) {
	config := *params.TestnetChainConfig
	config.TwoSecondsEpoch = big.NewInt(10)
	config.IsOneSecondEpoch = big.NewInt(20)

	tests := []struct {
		name               string
		currentEpoch       int64
		nextEpoch          int64
		isLastBlockInEpoch bool
		want               time.Duration
	}{
		{
			name:         "before two second activation",
			currentEpoch: 9,
			nextEpoch:    10,
			want:         5 * time.Second,
		},
		{
			name:               "two second activation boundary",
			currentEpoch:       9,
			nextEpoch:          10,
			isLastBlockInEpoch: true,
			want:               2 * time.Second,
		},
		{
			name:         "mid epoch before one second activation",
			currentEpoch: 19,
			nextEpoch:    20,
			want:         2 * time.Second,
		},
		{
			name:               "one second activation boundary",
			currentEpoch:       19,
			nextEpoch:          20,
			isLastBlockInEpoch: true,
			want:               time.Second,
		},
		{
			name:         "after one second activation",
			currentEpoch: 20,
			nextEpoch:    21,
			want:         time.Second,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := blockPeriodForConsensusUpdate(
				&config,
				big.NewInt(test.currentEpoch),
				big.NewInt(test.nextEpoch),
				test.isLastBlockInEpoch,
			)
			require.Equal(t, test.want, got)
		})
	}
}

func TestSetNextBlockDueUsesPeriodForNextConsensusEpoch(t *testing.T) {
	config := *params.TestnetChainConfig
	config.TwoSecondsEpoch = big.NewInt(10)
	config.IsOneSecondEpoch = big.NewInt(20)

	tests := []struct {
		name               string
		currentEpoch       int64
		nextEpoch          int64
		isLastBlockInEpoch bool
		want               time.Duration
	}{
		{
			name:         "mid epoch before two second activation",
			currentEpoch: 9,
			nextEpoch:    10,
			want:         5 * time.Second,
		},
		{
			name:               "first consensus at two second activation",
			currentEpoch:       9,
			nextEpoch:          10,
			isLastBlockInEpoch: true,
			want:               2 * time.Second,
		},
		{
			name:         "mid epoch before one second activation",
			currentEpoch: 19,
			nextEpoch:    20,
			want:         2 * time.Second,
		},
		{
			name:               "first consensus at one second activation",
			currentEpoch:       19,
			nextEpoch:          20,
			isLastBlockInEpoch: true,
			want:               time.Second,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			consensus := &Consensus{BlockPeriod: 99 * time.Second}
			now := time.Unix(100, 0)

			consensus.setNextBlockDue(
				now,
				&config,
				big.NewInt(test.currentEpoch),
				big.NewInt(test.nextEpoch),
				test.isLastBlockInEpoch,
			)

			require.Equal(t, test.want, consensus.BlockPeriod)
			require.Equal(t, now.Add(test.want), consensus.NextBlockDue)
		})
	}
}
