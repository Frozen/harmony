package engine

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestValidateBlockHashRejectsIncidentHistory(t *testing.T) {
	shard0Hash := common.HexToHash("0x5de06979a333f20afb8b245a8cf44472dc5bfc7383a57ddee48e1809bcee7c5d")
	shard1Hash := common.HexToHash("0xc936581d391b74a620bf6636519834b14a9a2d4e9a5154867c8407f219d8a878")
	tests := []struct {
		name    string
		shardID uint32
		number  uint64
		hash    common.Hash
		want    error
	}{
		{
			name: "reject shard 0 incident block", shardID: 0, number: 92730035,
			hash: shard0Hash, want: ErrRejectedBlock,
		},
		{
			name: "reject shard 1 incident block", shardID: 1, number: 94978279,
			hash: shard1Hash, want: ErrRejectedBlock,
		},
		{
			name: "allow replacement hash", shardID: 0, number: 92730035,
			hash: common.HexToHash("0x30c35d2f2291e4b27debe7862956cf7a0cc7abefc044273d6823567335086d8d"),
		},
		{name: "allow shard 0 hash on another shard", shardID: 1, number: 92730035, hash: shard0Hash},
		{name: "allow shard 0 hash at another height", shardID: 0, number: 92730034, hash: shard0Hash},
		{name: "allow shard 1 hash on another shard", shardID: 0, number: 94978279, hash: shard1Hash},
		{name: "allow shard 1 hash at another height", shardID: 1, number: 94978278, hash: shard1Hash},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateBlockHash(test.shardID, test.number, test.hash)
			if !errors.Is(err, test.want) {
				t.Fatalf("ValidateBlockHash(%d, %d, %s) error = %v, want %v", test.shardID, test.number, test.hash.Hex(), err, test.want)
			}
		})
	}
}
