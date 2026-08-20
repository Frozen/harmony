package consensus

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	blockfactory "github.com/harmony-one/harmony/block/factory"
	"github.com/harmony-one/harmony/internal/params"
	"github.com/harmony-one/harmony/shard"
	"github.com/stretchr/testify/require"
)

func TestVotingPowerContextForNextBlock(t *testing.T) {
	epoch := big.NewInt(3002)
	header := blockfactory.ForMainnet.NewHeader(epoch).With().
		Number(new(big.Int).SetUint64(92733438)).
		Epoch(epoch).
		ShardID(shard.BeaconChainShardID).
		ViewID(big.NewInt(1000003419)).
		ParentHash(common.HexToHash("0x01")).
		Header()

	ctx := votingPowerContextForNextBlock(params.MainnetChainConfig, header)
	require.Equal(t, uint64(92733439), ctx.BlockNumber)
	require.Equal(t, header.Hash(), ctx.ParentHash)
	require.Zero(t, ctx.ChainID.Cmp(params.MainnetChainID))
}
