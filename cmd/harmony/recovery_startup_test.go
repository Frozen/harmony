package main

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	blockfactory "github.com/harmony-one/harmony/block/factory"
	"github.com/harmony-one/harmony/core"
	"github.com/harmony-one/harmony/core/rawdb"
	corestate "github.com/harmony-one/harmony/core/state"
	"github.com/harmony-one/harmony/core/types"
	harmonyconfig "github.com/harmony-one/harmony/internal/configs/harmony"
	"github.com/harmony-one/harmony/internal/params"
	staketest "github.com/harmony-one/harmony/staking/types/test"
	"github.com/stretchr/testify/require"
)

type recoveryStartupTestChain struct {
	core.Stub
	config     *params.ChainConfig
	shardID    uint32
	current    *types.Block
	validators []common.Address
	stateDB    *corestate.DB
}

func (c *recoveryStartupTestChain) Config() *params.ChainConfig { return c.config }
func (c *recoveryStartupTestChain) ShardID() uint32             { return c.shardID }
func (c *recoveryStartupTestChain) CurrentBlock() *types.Block  { return c.current }
func (c *recoveryStartupTestChain) ReadValidatorList() ([]common.Address, error) {
	return c.validators, nil
}
func (c *recoveryStartupTestChain) StateAt(common.Hash) (*corestate.DB, error) {
	return c.stateDB, nil
}

func TestEmergencyRecoveryStartupSettings(t *testing.T) {
	mainnet := &recoveryStartupTestChain{config: params.MainnetChainConfig, shardID: 0}

	require.NoError(t, validateEmergencyRecoveryStartup(
		harmonyconfig.HarmonyConfig{General: harmonyconfig.GeneralConfig{IsOffline: true}}, nil,
	))
	require.NoError(t, validateEmergencyRecoveryStartup(
		harmonyconfig.HarmonyConfig{},
		&recoveryStartupTestChain{config: params.TestnetChainConfig, shardID: 0},
	))
	require.NoError(t, validateEmergencyRecoveryStartup(
		harmonyconfig.HarmonyConfig{},
		&recoveryStartupTestChain{config: params.MainnetChainConfig, shardID: 1},
	))

	tests := []struct {
		name   string
		config harmonyconfig.HarmonyConfig
	}{
		{name: "stream service", config: harmonyconfig.HarmonyConfig{Sync: harmonyconfig.SyncConfig{Enabled: true}}},
		{name: "stream client", config: harmonyconfig.HarmonyConfig{Sync: harmonyconfig.SyncConfig{Client: true}}},
		{name: "legacy client", config: harmonyconfig.HarmonyConfig{DNSSync: harmonyconfig.DnsSync{Client: true}}},
		{name: "legacy server", config: harmonyconfig.HarmonyConfig{DNSSync: harmonyconfig.DnsSync{Server: true}}},
		{name: "elastic mode", config: harmonyconfig.HarmonyConfig{General: harmonyconfig.GeneralConfig{RunElasticMode: true}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Error(t, validateEmergencyRecoveryStartup(test.config, mainnet))
		})
	}

	// With all jump/import modes disabled, a structurally empty chain still
	// fails the compiled checkpoint. This assertion remains valid after release
	// constants replace the development placeholders.
	require.Error(t, validateEmergencyRecoveryStartup(harmonyconfig.HarmonyConfig{}, mainnet))
}

func TestEmergencyRecoveryStartupRejectsUntrustedRawValidatorList(t *testing.T) {
	address := common.HexToAddress("0x0000000000000000000000000000000000000001")
	stateDB, err := corestate.New(common.Hash{}, corestate.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	require.NoError(t, err)
	wrapper := staketest.GetDefaultValidatorWrapperWithAddr(address, nil)
	wrapper.SlotPubKeys = staketest.GetDefaultValidatorWrapper().SlotPubKeys
	wrapper.CreationHeight = big.NewInt(1)
	require.NoError(t, stateDB.UpdateValidatorWrapper(address, &wrapper))
	stateDB.SetValidatorFlag(address)

	chain := &recoveryStartupTestChain{
		config:     params.MainnetChainConfig,
		shardID:    0,
		current:    types.NewBlockWithHeader(blockfactory.NewTestHeader()),
		validators: []common.Address{address},
		stateDB:    stateDB,
	}
	acceptManifest := func([]common.Address) error { return nil }
	require.NoError(t, validateEmergencyRecoveryStartupValidatorListWith(chain, acceptManifest))

	missingState, err := corestate.New(common.Hash{}, corestate.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	require.NoError(t, err)
	chain.stateDB = missingState
	require.Error(t, validateEmergencyRecoveryStartupValidatorListWith(chain, acceptManifest))
}

func TestEmergencyRecoveryNetworkIsolationIsForcedForMainnetShardZero(t *testing.T) {
	config := harmonyconfig.HarmonyConfig{
		Network: harmonyconfig.NetworkConfig{NetworkType: "mainnet"},
		Sync:    harmonyconfig.SyncConfig{Enabled: true, Client: true},
		DNSSync: harmonyconfig.DnsSync{Client: true, Server: true},
	}
	require.True(t, applyEmergencyRecoveryNetworkIsolation(&config, 0))
	require.False(t, config.Sync.Enabled)
	require.False(t, config.Sync.Client)
	require.False(t, config.DNSSync.Client)
	require.False(t, config.DNSSync.Server)

	testnet := harmonyconfig.HarmonyConfig{
		Network: harmonyconfig.NetworkConfig{NetworkType: "testnet"},
		Sync:    harmonyconfig.SyncConfig{Enabled: true, Client: true},
		DNSSync: harmonyconfig.DnsSync{Client: true, Server: true},
	}
	require.False(t, applyEmergencyRecoveryNetworkIsolation(&testnet, 0))
	require.True(t, testnet.Sync.Enabled)

	shardOne := harmonyconfig.HarmonyConfig{
		Network: harmonyconfig.NetworkConfig{NetworkType: "mainnet"},
		Sync:    harmonyconfig.SyncConfig{Enabled: true, Client: true},
		DNSSync: harmonyconfig.DnsSync{Client: true, Server: true},
	}
	require.False(t, applyEmergencyRecoveryNetworkIsolation(&shardOne, 1))
	require.True(t, shardOne.Sync.Enabled)
}
