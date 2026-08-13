package stagedstreamsync

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	blockfactory "github.com/harmony-one/harmony/block/factory"
	"github.com/harmony-one/harmony/core"
	"github.com/harmony-one/harmony/core/types"
)

type knownBlockChain struct {
	core.Stub
	hasBody   bool
	canonical *types.Block
	head      *types.Block
}

func (bc knownBlockChain) HasBlock(_ common.Hash, _ uint64) bool {
	return bc.hasBody
}

func (bc knownBlockChain) GetBlockByNumber(_ uint64) *types.Block {
	return bc.canonical
}

func (bc knownBlockChain) CurrentBlock() *types.Block {
	return bc.head
}

func TestIsKnownCanonicalBlockDoesNotAcceptStaleBody(t *testing.T) {
	candidate := types.NewBlockWithHeader(blockfactory.NewTestHeader().With().
		Number(big.NewInt(42)).
		Extra([]byte("candidate")).
		Header())
	replacement := types.NewBlockWithHeader(blockfactory.NewTestHeader().With().
		Number(big.NewInt(42)).
		Extra([]byte("replacement")).
		Header())
	head41 := types.NewBlockWithHeader(blockfactory.NewTestHeader().With().
		Number(big.NewInt(41)).
		Header())
	head42 := types.NewBlockWithHeader(blockfactory.NewTestHeader().With().
		Number(big.NewInt(42)).
		Header())

	tests := []struct {
		name string
		bc   knownBlockChain
		want bool
	}{
		{name: "canonical block at head", bc: knownBlockChain{hasBody: true, canonical: candidate, head: head42}, want: true},
		{name: "stale canonical mapping ahead of head", bc: knownBlockChain{hasBody: true, canonical: candidate, head: head41}, want: false},
		{name: "stale body after rollback", bc: knownBlockChain{hasBody: true, head: head41}, want: false},
		{name: "body from replaced history", bc: knownBlockChain{hasBody: true, canonical: replacement, head: head42}, want: false},
		{name: "body absent", bc: knownBlockChain{canonical: candidate, head: head42}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isKnownCanonicalBlock(test.bc, candidate); got != test.want {
				t.Fatalf("isKnownCanonicalBlock() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestUpdateBlockAndStatusReportsBlockNotInserted(t *testing.T) {
	head := types.NewBlockWithHeader(blockfactory.NewTestHeader().With().
		Number(big.NewInt(41)).
		Header())
	candidate := types.NewBlockWithHeader(blockfactory.NewTestHeader().With().
		Number(big.NewInt(43)).
		Header())

	inserted, err := new(StagedStreamSync).UpdateBlockAndStatus(
		candidate,
		knownBlockChain{head: head},
		false,
	)
	if err != nil {
		t.Fatalf("UpdateBlockAndStatus() error = %v", err)
	}
	if inserted {
		t.Fatal("UpdateBlockAndStatus() reported an inappropriate block as inserted")
	}
}
