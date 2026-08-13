package consensus

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	blockfactory "github.com/harmony-one/harmony/block/factory"
	consensusengine "github.com/harmony-one/harmony/consensus/engine"
	"github.com/harmony-one/harmony/core/types"
)

func TestValidateNewBlockRejectsIncidentBlockBeforeVerifiedCache(t *testing.T) {
	hash := common.HexToHash("0x890473cdb9aa8dc5c0bbd54cf20b6d8d84bda60d3dcb2273443d34432d8539e8")
	log := NewFBFTLog()
	log.verifiedBlocks[hash] = struct{}{}
	consensus := &Consensus{ShardID: 0, fBFTLog: log}
	consensus.current.phase.Store(FBFTAnnounce)
	block := types.NewBlockWithHeader(blockfactory.ForMainnet.NewHeader(big.NewInt(3002)).With().
		Number(new(big.Int).SetUint64(92730036)).
		ShardID(0).
		Header())
	payload, err := rlp.EncodeToBytes(block)
	if err != nil {
		t.Fatal(err)
	}

	_, err = consensus.validateNewBlock(&FBFTMessage{
		BlockNum:  92730036,
		BlockHash: hash,
		Block:     payload,
	})
	if !errors.Is(err, consensusengine.ErrRejectedBlock) {
		t.Fatalf("validateNewBlock() error = %v, want %v", err, consensusengine.ErrRejectedBlock)
	}
}
