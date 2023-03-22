package registry

import (
	"sync"

	"github.com/harmony-one/harmony/core"
)

// Registry consolidates services at one place.
type Registry struct {
	mu         sync.Mutex
	blockchain core.BlockChain
	epochchain core.BlockChain
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
