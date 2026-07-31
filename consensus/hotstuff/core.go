package hotstuff

import "errors"

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
	blocks    map[BlockID]Block
	committed BlockID
}

func NewCore(genesis Block) *Core {
	core := &Core{blocks: make(map[BlockID]Block)}
	if genesis.ID != "" && genesis.Parent == "" {
		core.blocks[genesis.ID] = genesis
		core.committed = genesis.ID
	}
	return core
}

func (c *Core) Committed() BlockID {
	return c.committed
}

// Accept validates the proposal's structural QC and applies the direct
// three-chain commit rule. Returned IDs are newly committed in chain order.
func (c *Core) Accept(block Block) ([]BlockID, error) {
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

	c.blocks[block.ID] = block

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
