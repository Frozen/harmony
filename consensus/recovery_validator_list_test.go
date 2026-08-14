package consensus

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
)

func recoveryValidatorListManifestForTest(
	t *testing.T, validators []common.Address,
) (uint64, string) {
	t.Helper()
	encoded, err := rlp.EncodeToBytes(validators)
	require.NoError(t, err)
	digest := sha256.Sum256(encoded)
	return uint64(len(validators)), "0x" + hex.EncodeToString(digest[:])
}

func TestEmergencyRecoveryValidatorListReleaseManifest(t *testing.T) {
	require.Equal(t, uint64(771), EmergencyRecoveryValidatorListCount)
	require.Equal(t,
		"0xf5dc6b4879ed956818c19d7e68b41044be251284d37b09735d896cc3d657050d",
		EmergencyRecoveryValidatorListSHA256Hex,
	)
	_, err := parseEmergencyRecoveryValidatorListDigest(EmergencyRecoveryValidatorListSHA256Hex)
	require.NoError(t, err)
}

func TestEmergencyRecoveryValidatorListManifestAcceptsExactOrderedList(t *testing.T) {
	validators := []common.Address{
		common.HexToAddress("0x0000000000000000000000000000000000000001"),
		common.HexToAddress("0x0000000000000000000000000000000000000002"),
	}
	count, digest := recoveryValidatorListManifestForTest(t, validators)
	computedCount, computedDigest, err := EmergencyRecoveryValidatorListManifest(validators)
	require.NoError(t, err)
	require.Equal(t, count, computedCount)
	require.Equal(t, digest, computedDigest)
	require.NoError(t, validateEmergencyRecoveryValidatorListWith(validators, count, digest))
}

func TestEmergencyRecoveryValidatorListManifestRejectsMismatch(t *testing.T) {
	validators := []common.Address{
		common.HexToAddress("0x0000000000000000000000000000000000000001"),
		common.HexToAddress("0x0000000000000000000000000000000000000002"),
	}
	count, digest := recoveryValidatorListManifestForTest(t, validators)

	t.Run("count", func(t *testing.T) {
		require.ErrorIs(t,
			validateEmergencyRecoveryValidatorListWith(validators[:1], count, digest),
			ErrEmergencyRecoveryValidatorListMismatch,
		)
	})

	t.Run("order", func(t *testing.T) {
		reordered := []common.Address{validators[1], validators[0]}
		require.ErrorIs(t,
			validateEmergencyRecoveryValidatorListWith(reordered, count, digest),
			ErrEmergencyRecoveryValidatorListMismatch,
		)
	})

	t.Run("address", func(t *testing.T) {
		changed := append([]common.Address(nil), validators...)
		changed[1] = common.HexToAddress("0x0000000000000000000000000000000000000003")
		require.ErrorIs(t,
			validateEmergencyRecoveryValidatorListWith(changed, count, digest),
			ErrEmergencyRecoveryValidatorListMismatch,
		)
	})
}

func TestEmergencyRecoveryValidatorListManifestFailsClosedWhenUnset(t *testing.T) {
	validators := []common.Address{
		common.HexToAddress("0x0000000000000000000000000000000000000001"),
	}
	_, digest := recoveryValidatorListManifestForTest(t, validators)
	zeroDigest := "0x" + hex.EncodeToString(make([]byte, sha256.Size))

	tests := []struct {
		name   string
		count  uint64
		digest string
	}{
		{name: "zero count", count: 0, digest: digest},
		{name: "placeholder digest", count: 1, digest: "REPLACE_WITH_TARGET_VALIDATOR_LIST_SHA256"},
		{name: "malformed digest", count: 1, digest: "0xnot-hex"},
		{name: "zero digest", count: 1, digest: zeroDigest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.ErrorIs(t,
				validateEmergencyRecoveryValidatorListWith(validators, test.count, test.digest),
				ErrEmergencyRecoveryValidatorListManifestUnset,
			)
		})
	}
}
