package config

import (
	"testing"

	nodeconfig "github.com/harmony-one/harmony/internal/configs/node"
	"github.com/stretchr/testify/require"
)

func TestRecoveryConfigAllowsNoSyncClientOnlyInNarrowCases(t *testing.T) {
	offline := GetDefaultHmyConfigCopy(nodeconfig.Mainnet)
	offline.General.IsOffline = true
	offline.P2P.IP = nodeconfig.DefaultLocalListenIP
	offline.Sync.Client = false
	offline.DNSSync.Client = false
	require.NoError(t, validateHarmonyConfig(offline))

	recoveryValidator := GetDefaultHmyConfigCopy(nodeconfig.Mainnet)
	recoveryValidator.Sync.Client = false
	recoveryValidator.DNSSync.Client = false
	require.NoError(t, validateHarmonyConfig(recoveryValidator))

	testnet := GetDefaultHmyConfigCopy(nodeconfig.Testnet)
	testnet.Sync.Client = false
	testnet.DNSSync.Client = false
	require.Error(t, validateHarmonyConfig(testnet))
}
