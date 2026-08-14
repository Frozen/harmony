package rawdb

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/harmony-one/harmony/core/types"
)

func TestReadBlockCommitSigExactNeverUsesLegacyFallback(t *testing.T) {
	db := NewMemoryDatabase()
	legacy := []byte("legacy-global-certificate")
	if err := db.Put(lastCommitsKey, legacy); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadBlockCommitSig(db, 92730034); err != nil || !bytes.Equal(got, legacy) {
		t.Fatalf("compatibility reader got %x, %v", got, err)
	}
	if got, err := ReadBlockCommitSigExact(db, 92730034); err == nil {
		t.Fatalf("exact reader accepted fallback bytes %x", got)
	}
	exact := []byte("height-keyed-certificate")
	if err := WriteBlockCommitSig(db, 92730034, exact); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadBlockCommitSigExact(db, 92730034); err != nil || !bytes.Equal(got, exact) {
		t.Fatalf("exact reader got %x, %v", got, err)
	}
}

func TestWriteCXReceiptsProofSpentUsesMerkleProofIdentity(t *testing.T) {
	db := NewMemoryDatabase()

	cxp := &types.CXReceiptsProof{
		MerkleProof: &types.CXMerkleProof{
			ShardID:  1,
			BlockNum: big.NewInt(999),
		},
	}

	if err := WriteCXReceiptsProofSpent(db, cxp); err != nil {
		t.Fatalf("failed to write spent marker: %v", err)
	}

	marker, err := ReadCXReceiptsProofSpent(db, cxp.MerkleProof.ShardID, cxp.MerkleProof.BlockNum.Uint64())
	if err != nil {
		t.Fatalf("failed to read spent marker keyed by merkle proof identity: %v", err)
	}
	if marker != SpentByte {
		t.Fatalf("wrong marker for merkle key: got %v want %v", marker, SpentByte)
	}
}

func TestWriteCXReceiptsProofSpentWithKey(t *testing.T) {
	db := NewMemoryDatabase()
	const shardID uint32 = 2
	const blockNum uint64 = 42

	if err := WriteCXReceiptsProofSpentWithKey(db, shardID, blockNum); err != nil {
		t.Fatalf("failed to write spent marker: %v", err)
	}
	marker, err := ReadCXReceiptsProofSpent(db, shardID, blockNum)
	if err != nil {
		t.Fatalf("failed to read spent marker: %v", err)
	}
	if marker != SpentByte {
		t.Fatalf("wrong marker value: got %v want %v", marker, SpentByte)
	}
}
