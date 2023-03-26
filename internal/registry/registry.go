package registry

import (
	"sync"

	"github.com/harmony-one/harmony/core"
	"github.com/harmony-one/harmony/p2p"
)

// Registry consolidates services at one place.
type Registry struct {
	mu                  sync.Mutex
	blockchain          core.BlockChain
	epochchain          core.BlockChain
	syncingPeerProvider SyncingPeerProvider
}

// New creates a new registry.
func New() *Registry {
	return &Registry{}
}

// SetBlockchain sets the blockchain to registry.
func (r *Registry) SetBlockchain(bc core.BlockChain) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.blockchain = bc
	return r
}

// GetBlockchain gets the blockchain from registry.
func (r *Registry) GetBlockchain() core.BlockChain {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.blockchain
}

// SetEpochChain sets the epochchain to registry.
func (r *Registry) SetEpochChain(ec core.BlockChain) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.epochchain = ec
	return r
}

// GetEpochChain gets the epochchain from registry.
func (r *Registry) GetEpochChain() core.BlockChain {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.epochchain
}

// SetSyncingPeerProvider sets the syncing peer provider to registry.
func (r *Registry) SetSyncingPeerProvider(spp SyncingPeerProvider) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.syncingPeerProvider = spp
	return r
}

// GetSyncingPeerProvider gets the syncing peer provider from registry.
func (r *Registry) GetSyncingPeerProvider() SyncingPeerProvider {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.syncingPeerProvider
}

// SyncingPeerProvider is an interface for getting the peers in the given shard.
type SyncingPeerProvider interface {
	SyncingPeers(shardID uint32) (peers []p2p.Peer, err error)
}
