package chain

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	blockfactory "github.com/harmony-one/harmony/block/factory"
	"github.com/harmony-one/harmony/core/types"
	bls "github.com/harmony-one/harmony/crypto/bls"
	"github.com/harmony-one/harmony/shard"
	"github.com/stretchr/testify/require"
)

func TestVerifiedSignatureCacheKeyIncludesVotingPowerContext(t *testing.T) {
	chainID := big.NewInt(1)
	pas := payloadArgs{
		blockHash:  common.HexToHash("0x01"),
		parentHash: common.HexToHash("0x02"),
		shardID:    0,
		epoch:      big.NewInt(3002),
		number:     92733439,
		viewID:     1000003642,
	}
	var sig bls.SerializedSignature
	bitmap := []byte{0x01}
	base := newVerifiedSigKey(chainID, pas, sig, bitmap)
	require.NotEqual(t, base, newVerifiedSigKey(chainID, pas, sig, append(bitmap, 0)), "bitmap length must be part of the cache key")

	mutations := []struct {
		name    string
		chainID *big.Int
		pas     payloadArgs
	}{
		{name: "network", chainID: big.NewInt(2), pas: pas},
		{name: "shard", chainID: chainID, pas: func() payloadArgs { p := pas; p.shardID++; return p }()},
		{name: "epoch", chainID: chainID, pas: func() payloadArgs { p := pas; p.epoch = big.NewInt(3003); return p }()},
		{name: "number", chainID: chainID, pas: func() payloadArgs { p := pas; p.number++; return p }()},
		{name: "view", chainID: chainID, pas: func() payloadArgs { p := pas; p.viewID++; return p }()},
		{name: "block hash", chainID: chainID, pas: func() payloadArgs { p := pas; p.blockHash = common.HexToHash("0x03"); return p }()},
		{name: "parent hash", chainID: chainID, pas: func() payloadArgs { p := pas; p.parentHash = common.HexToHash("0x04"); return p }()},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			require.NotEqual(t, base, newVerifiedSigKey(mutation.chainID, mutation.pas, sig, bitmap))
		})
	}
}

func TestPayloadArgsFromHeaderCarriesEmergencyQuorumContext(t *testing.T) {
	parentHash := common.HexToHash("0xcbaceb7635b4e2d612c21b34fe24f308076e25b02d6379be17150f77b86f8f32")
	epoch := big.NewInt(3002)
	header := blockfactory.ForMainnet.NewHeader(epoch).With().
		Number(new(big.Int).SetUint64(92733439)).
		Epoch(epoch).
		ShardID(shard.BeaconChainShardID).
		ViewID(big.NewInt(1000003642)).
		ParentHash(parentHash).
		Header()

	args := payloadArgsFromHeader(header)
	require.Equal(t, uint64(92733439), args.number)
	require.Equal(t, parentHash, args.parentHash)
}

func TestPayloadArgsFromCrossLinkRecoversParentHash(t *testing.T) {
	chain := makeFakeBlockChain()
	header := chain.CurrentHeader()
	cl := types.CrossLink{
		HashF:        header.Hash(),
		BlockNumberF: header.Number(),
		ViewIDF:      header.ViewID(),
		ShardIDF:     header.ShardID(),
		EpochF:       header.Epoch(),
	}

	args := payloadArgsFromCrossLink(chain, cl)
	require.Equal(t, header.ParentHash(), args.parentHash)
}
