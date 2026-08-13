package engine

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/harmony-one/harmony/block"
	blockfactory "github.com/harmony-one/harmony/block/factory"
	"github.com/harmony-one/harmony/internal/params"
)

func TestValidateBlockHashRejectsIncidentHistory(t *testing.T) {
	shard0Hash := common.HexToHash("0x890473cdb9aa8dc5c0bbd54cf20b6d8d84bda60d3dcb2273443d34432d8539e8")
	shard1Hash := common.HexToHash("0xc936581d391b74a620bf6636519834b14a9a2d4e9a5154867c8407f219d8a878")
	tests := []struct {
		name    string
		shardID uint32
		number  uint64
		hash    common.Hash
		want    error
	}{
		{
			name: "reject shard 0 incident block", shardID: 0, number: 92730036,
			hash: shard0Hash, want: ErrRejectedBlock,
		},
		{
			name: "reject shard 1 incident block", shardID: 1, number: 94978279,
			hash: shard1Hash, want: ErrRejectedBlock,
		},
		{name: "allow replacement hash", shardID: 0, number: 92730036, hash: common.HexToHash("0x01")},
		{name: "allow shard 0 hash on another shard", shardID: 1, number: 92730036, hash: shard0Hash},
		{name: "allow shard 0 hash at another height", shardID: 0, number: 92730035, hash: shard0Hash},
		{name: "allow shard 1 hash on another shard", shardID: 0, number: 94978279, hash: shard1Hash},
		{name: "allow shard 1 hash at another height", shardID: 1, number: 94978278, hash: shard1Hash},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateBlockHash(nil, test.shardID, test.number, test.hash)
			if !errors.Is(err, test.want) {
				t.Fatalf("ValidateBlockHash(%d, %d, %s) error = %v, want %v", test.shardID, test.number, test.hash.Hex(), err, test.want)
			}
		})
	}
}

func TestValidateRecoveryCheckpoint(t *testing.T) {
	checkpoint := recoveryCheckpoints[0]
	if err := ValidateRecoveryCheckpoint(nil, 0, checkpoint.number, checkpoint.hash, checkpoint.root); err != nil {
		t.Fatalf("valid checkpoint rejected: %v", err)
	}
	if err := ValidateRecoveryCheckpoint(nil, 0, checkpoint.number, checkpoint.hash, common.HexToHash("0x01")); !errors.Is(err, ErrRejectedBlock) {
		t.Fatalf("wrong checkpoint root error = %v, want %v", err, ErrRejectedBlock)
	}
	if err := ValidateRecoveryCheckpoint(nil, 0, checkpoint.number, common.HexToHash("0x01"), checkpoint.root); !errors.Is(err, ErrRejectedBlock) {
		t.Fatalf("wrong checkpoint hash error = %v, want %v", err, ErrRejectedBlock)
	}
	if err := ValidateRecoveryCheckpoint(nil, 0, checkpoint.number+1, common.Hash{}, common.Hash{}); err != nil {
		t.Fatalf("replacement height rejected: %v", err)
	}
}

type recoveryNetworkReader struct {
	ChainReader
	config *params.ChainConfig
}

func (chain recoveryNetworkReader) Config() *params.ChainConfig { return chain.config }

func TestRecoveryRulesOnlyApplyToMainnet(t *testing.T) {
	for _, config := range []*params.ChainConfig{params.TestnetChainConfig, params.LocalnetChainConfig} {
		chain := recoveryNetworkReader{config: config}
		rejected := rejectedBlock{0, 92730036, common.HexToHash("0x890473cdb9aa8dc5c0bbd54cf20b6d8d84bda60d3dcb2273443d34432d8539e8")}
		if err := ValidateBlockHash(chain, rejected.shardID, rejected.number, rejected.hash); err != nil {
			t.Fatalf("non-mainnet block rejected: %v", err)
		}
		checkpoint := recoveryCheckpoints[0]
		if err := ValidateRecoveryCheckpoint(chain, 0, checkpoint.number, common.Hash{}, common.Hash{}); err != nil {
			t.Fatalf("non-mainnet checkpoint rejected: %v", err)
		}
	}
}

type recoveryChainReader struct {
	ChainReader
	headers map[rejectedBlock]*block.Header
}

func (chain recoveryChainReader) CurrentHeader() *block.Header { return nil }

func (chain recoveryChainReader) Config() *params.ChainConfig { return params.MainnetChainConfig }

func (chain recoveryChainReader) GetHeader(hash common.Hash, number uint64) *block.Header {
	return chain.headers[rejectedBlock{number: number, hash: hash}]
}

func TestValidateRecoveryAncestry(t *testing.T) {
	checkpoint := recoveryCheckpoints[0]
	retained := blockfactory.NewTestHeader()
	retained.SetShardID(0)
	retained.SetNumber(new(big.Int).SetUint64(checkpoint.number))
	retained.SetRoot(checkpoint.root)

	parent := blockfactory.NewTestHeader()
	parent.SetShardID(0)
	parent.SetNumber(new(big.Int).SetUint64(checkpoint.number + 1))
	parent.SetParentHash(checkpoint.hash)

	child := blockfactory.NewTestHeader()
	child.SetShardID(0)
	child.SetNumber(new(big.Int).SetUint64(checkpoint.number + 2))
	child.SetParentHash(parent.Hash())

	valid := recoveryChainReader{headers: map[rejectedBlock]*block.Header{
		{number: checkpoint.number + 1, hash: parent.Hash()}: parent,
		{number: checkpoint.number, hash: checkpoint.hash}:   retained,
	}}
	if err := ValidateRecoveryAncestry(valid, child); err != nil {
		t.Fatalf("valid replacement ancestry rejected: %v", err)
	}

	abandonedHash := common.HexToHash("0x01")
	abandonedParent := blockfactory.NewTestHeader()
	abandonedParent.SetShardID(0)
	abandonedParent.SetNumber(new(big.Int).SetUint64(checkpoint.number + 1))
	abandonedParent.SetParentHash(abandonedHash)
	abandoned := recoveryChainReader{headers: map[rejectedBlock]*block.Header{
		{number: checkpoint.number + 1, hash: abandonedParent.Hash()}: abandonedParent,
		{number: checkpoint.number, hash: abandonedHash}:              retained,
	}}
	abandonedChild := blockfactory.NewTestHeader()
	abandonedChild.SetShardID(0)
	abandonedChild.SetNumber(new(big.Int).SetUint64(checkpoint.number + 2))
	abandonedChild.SetParentHash(abandonedParent.Hash())
	if err := ValidateRecoveryAncestry(abandoned, abandonedChild); !errors.Is(err, ErrRejectedBlock) {
		t.Fatalf("abandoned ancestry error = %v, want %v", err, ErrRejectedBlock)
	}

	pruned := recoveryChainReader{headers: map[rejectedBlock]*block.Header{
		{number: checkpoint.number + 1, hash: parent.Hash()}: parent,
	}}
	if err := ValidateRecoveryAncestry(pruned, child); err != nil {
		t.Fatalf("pruned deep ancestry rejected: %v", err)
	}
}
