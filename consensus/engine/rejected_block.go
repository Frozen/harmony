package engine

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/harmony-one/harmony/block"
	"github.com/harmony-one/harmony/internal/params"
)

// ErrRejectedBlock is returned for a block that consensus must never accept.
var ErrRejectedBlock = errors.New("block rejected by hash")

type rejectedBlock struct {
	shardID uint32
	number  uint64
	hash    common.Hash
}

type recoveryCheckpoint struct {
	number uint64
	hash   common.Hash
	root   common.Hash
}

var rejectedBlocks = map[rejectedBlock]struct{}{
	{0, 92730036, common.HexToHash("0x890473cdb9aa8dc5c0bbd54cf20b6d8d84bda60d3dcb2273443d34432d8539e8")}: {},
	{1, 94978279, common.HexToHash("0xc936581d391b74a620bf6636519834b14a9a2d4e9a5154867c8407f219d8a878")}: {},
}

var recoveryCheckpoints = map[uint32]recoveryCheckpoint{
	0: {
		number: 92730035,
		hash:   common.HexToHash("0x5de06979a333f20afb8b245a8cf44472dc5bfc7383a57ddee48e1809bcee7c5d"),
		root:   common.HexToHash("0x39e72dc20835abe61f69966bec2cc4766bb9e893c4168e117154dd539f2fc728"),
	},
	1: {
		number: 94978278,
		hash:   common.HexToHash("0xa25d77e72c7f71f2b18847c7f6a9bbed8af42244915bd9175cc247d157b11b9f"),
		root:   common.HexToHash("0x312a34c0254608c59013bc967c486fe036b784e0d2012b266d5fa4ecc2531760"),
	},
}

// ValidateBlockHash rejects blocks that must not be accepted after a chain rollback.
func isRecoveryNetwork(chain ChainReader) bool {
	return chain == nil || chain.Config() != nil && chain.Config().ChainID.Cmp(params.MainnetChainID) == 0
}

func ValidateBlockHash(chain ChainReader, shardID uint32, number uint64, hash common.Hash) error {
	if !isRecoveryNetwork(chain) {
		return nil
	}
	if _, rejected := rejectedBlocks[rejectedBlock{shardID, number, hash}]; rejected {
		return fmt.Errorf("%w: shard %d block %d %s", ErrRejectedBlock, shardID, number, hash.Hex())
	}
	return nil
}

// ValidateRecoveryCheckpoint enforces the exact retained block and state root.
func ValidateRecoveryCheckpoint(chain ChainReader, shardID uint32, number uint64, hash, root common.Hash) error {
	if !isRecoveryNetwork(chain) {
		return nil
	}
	checkpoint, ok := recoveryCheckpoints[shardID]
	if !ok || number != checkpoint.number {
		return nil
	}
	if hash != checkpoint.hash || root != checkpoint.root {
		return fmt.Errorf(
			"%w: shard %d checkpoint %d have hash %s root %s want hash %s root %s",
			ErrRejectedBlock, shardID, number, hash.Hex(), root.Hex(), checkpoint.hash.Hex(), checkpoint.root.Hex(),
		)
	}
	return nil
}

// ValidateRecoveryAncestry rejects history above an incident checkpoint unless
// it descends from the exact retained checkpoint.
func ValidateRecoveryAncestry(chain ChainReader, header *block.Header) error {
	if header == nil {
		return nil
	}
	checkpoint, ok := recoveryCheckpoints[header.ShardID()]
	if !ok || header.Number().Uint64() <= checkpoint.number {
		return nil
	}

	current := chain.CurrentHeader()
	hash := header.ParentHash()
	number := header.Number().Uint64() - 1
	for number >= checkpoint.number {
		if current != nil && number == current.Number().Uint64() && hash == current.Hash() {
			return nil
		}
		ancestor := chain.GetHeader(hash, number)
		if ancestor == nil {
			if number == header.Number().Uint64()-1 {
				return ErrUnknownAncestor
			}
			return nil
		}
		if err := ValidateBlockHash(chain, ancestor.ShardID(), number, hash); err != nil {
			return err
		}
		if err := ValidateRecoveryCheckpoint(chain, ancestor.ShardID(), number, hash, ancestor.Root()); err != nil {
			return err
		}
		if number == checkpoint.number {
			return nil
		}
		hash = ancestor.ParentHash()
		number--
	}
	return ErrUnknownAncestor
}
