package engine

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// ErrRejectedBlock is returned for a block that consensus must never accept.
var ErrRejectedBlock = errors.New("block rejected by hash")

type rejectedBlock struct {
	shardID uint32
	number  uint64
	hash    common.Hash
}

var rejectedBlocks = map[rejectedBlock]struct{}{
	{0, 92730035, common.HexToHash("0x5de06979a333f20afb8b245a8cf44472dc5bfc7383a57ddee48e1809bcee7c5d")}: {},
	{1, 94978279, common.HexToHash("0xc936581d391b74a620bf6636519834b14a9a2d4e9a5154867c8407f219d8a878")}: {},
}

// ValidateBlockHash rejects blocks that must not be accepted after a chain rollback.
func ValidateBlockHash(shardID uint32, number uint64, hash common.Hash) error {
	if _, rejected := rejectedBlocks[rejectedBlock{shardID, number, hash}]; rejected {
		return fmt.Errorf("%w: shard %d block %d %s", ErrRejectedBlock, shardID, number, hash.Hex())
	}
	return nil
}
