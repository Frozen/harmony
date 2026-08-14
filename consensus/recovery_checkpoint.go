package consensus

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	consensusengine "github.com/harmony-one/harmony/consensus/engine"
	"github.com/harmony-one/harmony/core"
	"github.com/harmony-one/harmony/core/rawdb"
	"github.com/harmony-one/harmony/crypto/bls"
	"github.com/harmony-one/harmony/internal/params"
	"github.com/harmony-one/harmony/shard"
)

const (
	// These values pin the independently verified block-92730034 tuple used by
	// the shard-0 emergency recovery release.
	EmergencyRecoveryRetainedHashHex = "0x30c35d2f2291e4b27debe7862956cf7a0cc7abefc044273d6823567335086d8d"
	EmergencyRecoveryRetainedRootHex = "0x39e72dc20835abe61f69966bec2cc4766bb9e893c4168e117154dd539f2fc728"
)

var (
	ErrEmergencyRecoveryCheckpointUnset    = errors.New("emergency recovery checkpoint is unset")
	ErrEmergencyRecoveryCheckpointMismatch = errors.New("emergency recovery checkpoint mismatch")
)

// IsEmergencyRecoveryMainnetShard0 scopes the one-off startup invariant. It is
// intentionally independent of the local head height: a head below the target
// must fail rather than bypass recovery checks.
func IsEmergencyRecoveryMainnetShard0(config *params.ChainConfig, shardID uint32) bool {
	return config != nil && config.ChainID != nil &&
		config.ChainID.Cmp(params.MainnetChainID) == 0 &&
		shardID == shard.BeaconChainShardID
}

func parseRecoveryHash(name, value string) (common.Hash, error) {
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return common.Hash{}, fmt.Errorf("%w: %s", ErrEmergencyRecoveryCheckpointUnset, name)
	}
	raw, err := hex.DecodeString(value[2:])
	if err != nil || len(raw) != common.HashLength {
		return common.Hash{}, fmt.Errorf("%w: %s", ErrEmergencyRecoveryCheckpointUnset, name)
	}
	var result common.Hash
	copy(result[:], raw)
	if result == (common.Hash{}) {
		return common.Hash{}, fmt.Errorf("%w: %s is zero", ErrEmergencyRecoveryCheckpointUnset, name)
	}
	return result, nil
}

// EmergencyRecoveryCheckpoint returns the release-pinned retained block tuple.
// It fails closed while either value is still a release placeholder.
func EmergencyRecoveryCheckpoint() (common.Hash, common.Hash, error) {
	targetHash, err := parseRecoveryHash("target block hash", EmergencyRecoveryRetainedHashHex)
	if err != nil {
		return common.Hash{}, common.Hash{}, err
	}
	targetRoot, err := parseRecoveryHash("target state root", EmergencyRecoveryRetainedRootHex)
	if err != nil {
		return common.Hash{}, common.Hash{}, err
	}
	return targetHash, targetRoot, nil
}

// ValidateEmergencyRecoveryCheckpoint rejects a stale or partially rewound DB
// before networking, services, or validator signing start.
func ValidateEmergencyRecoveryCheckpoint(blockchain core.BlockChain) error {
	if blockchain == nil {
		return errors.New("nil blockchain")
	}
	if !IsEmergencyRecoveryMainnetShard0(blockchain.Config(), blockchain.ShardID()) {
		return nil
	}
	targetHash, targetRoot, err := EmergencyRecoveryCheckpoint()
	if err != nil {
		return err
	}
	if err := validateEmergencyRecoveryCheckpointWith(
		blockchain, targetHash, targetRoot, consensusengine.ValidateBlockHash,
	); err != nil {
		return err
	}
	return validateEmergencyRecoveryTargetCertificate(blockchain, targetHash)
}

func validateEmergencyRecoveryTargetCertificate(blockchain core.BlockChain, targetHash common.Hash) error {
	target := blockchain.GetBlock(targetHash, EmergencyRecoveryRetainedBlock)
	if target == nil || target.Header() == nil {
		return fmt.Errorf("%w: target block missing for certificate validation",
			ErrEmergencyRecoveryCheckpointMismatch)
	}
	certificate, err := rawdb.ReadBlockCommitSigExact(
		blockchain.ChainDb(), EmergencyRecoveryRetainedBlock,
	)
	if err != nil {
		return fmt.Errorf("%w: target commit certificate unavailable: %v",
			ErrEmergencyRecoveryCheckpointMismatch, err)
	}
	if len(certificate) <= bls.BLSSignatureSizeInBytes {
		return fmt.Errorf("%w: target commit certificate is truncated",
			ErrEmergencyRecoveryCheckpointMismatch)
	}
	var signature bls.SerializedSignature
	copy(signature[:], certificate[:bls.BLSSignatureSizeInBytes])
	bitmap := certificate[bls.BLSSignatureSizeInBytes:]
	if blockchain.Engine() == nil {
		return fmt.Errorf("%w: consensus engine unavailable",
			ErrEmergencyRecoveryCheckpointMismatch)
	}
	if err := blockchain.Engine().VerifyHeaderSignature(blockchain, target.Header(), signature, bitmap); err != nil {
		return fmt.Errorf("%w: target commit certificate is invalid: %v",
			ErrEmergencyRecoveryCheckpointMismatch, err)
	}

	return nil
}

func validateEmergencyRecoveryCheckpointWith(
	blockchain core.BlockChain,
	targetHash common.Hash,
	targetRoot common.Hash,
	validateHash func(common.Hash) error,
) error {
	current := blockchain.CurrentBlock()
	fast := blockchain.CurrentFastBlock()
	header := blockchain.CurrentHeader()
	if current == nil || fast == nil || header == nil || current.Header() == nil {
		return fmt.Errorf("%w: a chain head is missing", ErrEmergencyRecoveryCheckpointMismatch)
	}
	headNumber := current.NumberU64()
	if headNumber < EmergencyRecoveryRetainedBlock {
		return fmt.Errorf("%w: head %d is below target %d",
			ErrEmergencyRecoveryCheckpointMismatch, headNumber, EmergencyRecoveryRetainedBlock)
	}
	if current.ShardID() != shard.BeaconChainShardID ||
		header.Number().Uint64() != headNumber || header.Hash() != current.Hash() ||
		fast.NumberU64() != headNumber || fast.Hash() != current.Hash() {
		return fmt.Errorf("%w: full, header, and fast heads are not identical",
			ErrEmergencyRecoveryCheckpointMismatch)
	}
	if blockchain.GetCanonicalHash(headNumber) != current.Hash() {
		return fmt.Errorf("%w: current head is not canonical", ErrEmergencyRecoveryCheckpointMismatch)
	}
	if blockchain.GetCanonicalHash(EmergencyRecoveryRetainedBlock) != targetHash {
		return fmt.Errorf("%w: target canonical hash", ErrEmergencyRecoveryCheckpointMismatch)
	}

	walk := current
	for {
		if walk == nil || walk.Header() == nil || walk.NumberU64() > headNumber ||
			walk.ShardID() != shard.BeaconChainShardID {
			return fmt.Errorf("%w: broken ancestry", ErrEmergencyRecoveryCheckpointMismatch)
		}
		if err := validateHash(walk.Hash()); err != nil {
			return err
		}
		if walk.NumberU64() == EmergencyRecoveryRetainedBlock {
			break
		}
		parentNumber := walk.NumberU64() - 1
		parent := blockchain.GetBlock(walk.ParentHash(), parentNumber)
		if parent == nil || parent.Hash() != walk.ParentHash() || parent.NumberU64() != parentNumber {
			return fmt.Errorf("%w: missing parent at %d",
				ErrEmergencyRecoveryCheckpointMismatch, parentNumber)
		}
		walk = parent
	}

	if walk.Hash() != targetHash || walk.Root() != targetRoot {
		return fmt.Errorf("%w: target hash or state root", ErrEmergencyRecoveryCheckpointMismatch)
	}
	if blockchain.GetBlock(targetHash, EmergencyRecoveryRetainedBlock) == nil {
		return fmt.Errorf("%w: target block body missing", ErrEmergencyRecoveryCheckpointMismatch)
	}
	stateDB, err := blockchain.StateAt(current.Root())
	if err != nil || stateDB == nil {
		return fmt.Errorf("%w: current state unavailable: %v",
			ErrEmergencyRecoveryCheckpointMismatch, err)
	}
	return nil
}
