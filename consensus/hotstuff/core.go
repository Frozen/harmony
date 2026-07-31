package hotstuff

import (
	"errors"
	"sync"
)

var (
	ErrInvalidGenesis         = errors.New("hotstuff genesis block is invalid")
	ErrDuplicateBlock         = errors.New("hotstuff block already exists")
	ErrUnknownParent          = errors.New("hotstuff block parent is unknown")
	ErrUnknownQCBlock         = errors.New("hotstuff QC block is unknown")
	ErrQCDoesNotJustifyParent = errors.New("hotstuff QC does not justify the proposal parent")
	ErrInvalidView            = errors.New("hotstuff proposal view does not advance its parent")
	ErrQCViewMismatch         = errors.New("hotstuff QC view does not match its block")
)

// Core is a transport- and cryptography-independent chained HotStuff spike. It
// accepts only direct parent/QC links so the three-chain commit rule is
// explicit and easy to test before adding skipped views.
type Core struct {
	mu        sync.RWMutex
	blocks    map[BlockID]Block
	committed BlockID
}

func NewCore(genesis Block) *Core {
	core := &Core{blocks: make(map[BlockID]Block)}
	if genesis.ID != "" && genesis.Parent == "" {
		core.blocks[genesis.ID] = cloneBlock(genesis)
		core.committed = genesis.ID
	}
	return core
}

func (c *Core) Committed() BlockID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.committed
}

func (c *Core) block(id BlockID) (Block, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	block, exists := c.blocks[id]
	return cloneBlock(block), exists
}

// Extends reports whether descendant is on the branch rooted at ancestor.
func (c *Core) Extends(descendant, ancestor BlockID) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for descendant != "" {
		if descendant == ancestor {
			return true
		}
		block, exists := c.blocks[descendant]
		if !exists {
			return false
		}
		descendant = block.Parent
	}
	return false
}

// lockQC returns the QC that becomes locked when proposal completes a direct
// two-chain. The caller is responsible for retaining the highest lock.
func (c *Core) lockQC(proposal Block) (QC, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	parent, exists := c.blocks[proposal.Parent]
	if !exists || proposal.Justify.Block != parent.ID {
		return QC{}, false
	}
	grandparent, exists := c.blocks[parent.Parent]
	if !exists || parent.Justify.Block != grandparent.ID {
		return QC{}, false
	}
	return cloneQC(parent.Justify), true
}

// Accept validates the proposal's structural QC and applies the direct
// three-chain commit rule. Returned IDs are newly committed in chain order.
func (c *Core) Accept(block Block) ([]BlockID, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.blocks) == 0 {
		return nil, ErrInvalidGenesis
	}
	if block.ID == "" {
		return nil, ErrUnknownParent
	}
	if _, exists := c.blocks[block.ID]; exists {
		return nil, ErrDuplicateBlock
	}

	parent, exists := c.blocks[block.Parent]
	if !exists {
		return nil, ErrUnknownParent
	}
	qcBlock, exists := c.blocks[block.Justify.Block]
	if !exists {
		return nil, ErrUnknownQCBlock
	}
	if block.Justify.Block != block.Parent {
		return nil, ErrQCDoesNotJustifyParent
	}
	if block.Justify.View != qcBlock.View {
		return nil, ErrQCViewMismatch
	}
	if block.View <= parent.View {
		return nil, ErrInvalidView
	}

	c.blocks[block.ID] = cloneBlock(block)

	one := parent
	two, ok := c.blocks[one.Parent]
	if !ok || one.Justify.Block != two.ID {
		return nil, nil
	}
	three, ok := c.blocks[two.Parent]
	if !ok || two.Justify.Block != three.ID {
		return nil, nil
	}

	return c.commitThrough(three.ID), nil
}

func (c *Core) commitThrough(target BlockID) []BlockID {
	if target == c.committed {
		return nil
	}

	path := make([]BlockID, 0)
	for current := target; current != c.committed; {
		block, exists := c.blocks[current]
		if !exists {
			return nil
		}
		path = append(path, current)
		current = block.Parent
	}

	for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
		path[left], path[right] = path[right], path[left]
	}
	c.committed = target
	return path
}

func cloneBlock(block Block) Block {
	block.Justify = cloneQC(block.Justify)
	return block
}
