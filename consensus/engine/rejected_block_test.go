package engine

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestValidateBlockHashRejectsAbandonedChainAnchors(t *testing.T) {
	tests := []struct {
		name string
		hash common.Hash
		want error
	}{
		{
			name: "reject shard 0 abandoned chain anchor",
			hash: common.HexToHash("0x890473cdb9aa8dc5c0bbd54cf20b6d8d84bda60d3dcb2273443d34432d8539e8"),
			want: ErrRejectedBlock,
		},
		{
			name: "reject shard 1 abandoned chain anchor",
			hash: common.HexToHash("0xc936581d391b74a620bf6636519834b14a9a2d4e9a5154867c8407f219d8a878"),
			want: ErrRejectedBlock,
		},
		{name: "allow replacement hash", hash: common.HexToHash("0x01")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateBlockHash(test.hash)
			if !errors.Is(err, test.want) {
				t.Fatalf("ValidateBlockHash(%s) error = %v, want %v", test.hash.Hex(), err, test.want)
			}
		})
	}
}
