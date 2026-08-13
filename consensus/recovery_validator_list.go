package consensus

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	// TODO(recovery-release): These values are release blockers. Replace them
	// with the independently audited validator list at block 92730034 before
	// producing the recovery binary. The digest is SHA-256 over the RLP encoding
	// of []common.Address, exactly matching the canonical encoding stored by
	// rawdb.WriteValidatorList.
	EmergencyRecoveryValidatorListCount     uint64 = 0
	EmergencyRecoveryValidatorListSHA256Hex        = "REPLACE_WITH_TARGET_VALIDATOR_LIST_SHA256"
)

var (
	ErrEmergencyRecoveryValidatorListManifestUnset = errors.New("emergency recovery validator-list manifest is unset")
	ErrEmergencyRecoveryValidatorListMismatch      = errors.New("emergency recovery validator-list mismatch")
)

// ValidateEmergencyRecoveryValidatorList verifies the exact ordered validator
// list against the release manifest. It fails closed while either release
// value is left as a placeholder.
func ValidateEmergencyRecoveryValidatorList(validators []common.Address) error {
	return validateEmergencyRecoveryValidatorListWith(
		validators,
		EmergencyRecoveryValidatorListCount,
		EmergencyRecoveryValidatorListSHA256Hex,
	)
}

// EmergencyRecoveryValidatorListManifest computes the release-manifest values
// from the exact ordered list. It is exposed so offline recovery preflight can
// report the independently reproducible candidate before any database write.
func EmergencyRecoveryValidatorListManifest(validators []common.Address) (uint64, string, error) {
	encoded, err := rlp.EncodeToBytes(validators)
	if err != nil {
		return 0, "", err
	}
	digest := sha256.Sum256(encoded)
	return uint64(len(validators)), "0x" + hex.EncodeToString(digest[:]), nil
}

func validateEmergencyRecoveryValidatorListWith(
	validators []common.Address,
	expectedCount uint64,
	expectedSHA256Hex string,
) error {
	if expectedCount == 0 {
		return fmt.Errorf("%w: validator count", ErrEmergencyRecoveryValidatorListManifestUnset)
	}
	expectedDigest, err := parseEmergencyRecoveryValidatorListDigest(expectedSHA256Hex)
	if err != nil {
		return err
	}
	if uint64(len(validators)) != expectedCount {
		return fmt.Errorf("%w: count got %d want %d",
			ErrEmergencyRecoveryValidatorListMismatch, len(validators), expectedCount)
	}

	encoded, err := rlp.EncodeToBytes(validators)
	if err != nil {
		return fmt.Errorf("%w: RLP encoding failed: %v",
			ErrEmergencyRecoveryValidatorListMismatch, err)
	}
	digest := sha256.Sum256(encoded)
	if digest != expectedDigest {
		return fmt.Errorf("%w: SHA-256 got 0x%s want 0x%s",
			ErrEmergencyRecoveryValidatorListMismatch,
			hex.EncodeToString(digest[:]), hex.EncodeToString(expectedDigest[:]))
	}
	return nil
}

func parseEmergencyRecoveryValidatorListDigest(value string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	if len(value) != 2+sha256.Size*2 || !strings.HasPrefix(value, "0x") {
		return digest, fmt.Errorf("%w: validator-list SHA-256",
			ErrEmergencyRecoveryValidatorListManifestUnset)
	}
	raw, err := hex.DecodeString(value[2:])
	if err != nil || len(raw) != sha256.Size {
		return digest, fmt.Errorf("%w: validator-list SHA-256",
			ErrEmergencyRecoveryValidatorListManifestUnset)
	}
	copy(digest[:], raw)
	if digest == ([sha256.Size]byte{}) {
		return digest, fmt.Errorf("%w: validator-list SHA-256 is zero",
			ErrEmergencyRecoveryValidatorListManifestUnset)
	}
	return digest, nil
}
