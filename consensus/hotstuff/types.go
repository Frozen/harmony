package hotstuff

// View identifies a HotStuff consensus round. A successful view produces at
// most one block and has exactly one scheduled leader.
type View uint64

// MemberID identifies a committee member. The production adapter will map this
// value to a Harmony validator identity and its BLS keys.
type MemberID string

// BlockID identifies a proposal. The production adapter will use a block hash.
type BlockID string

// QC is the structural part of a quorum certificate. Signatures are
// intentionally left to the Harmony BLS adapter.
type QC struct {
	Block   BlockID
	View    View
	Signers []MemberID
}

// Block is the minimum chained HotStuff proposal needed by the spike.
type Block struct {
	ID      BlockID
	Parent  BlockID
	View    View
	Justify QC
}

// Vote is broadcast to the committee in the first transport experiment.
type Vote struct {
	Voter MemberID
	Block BlockID
	View  View
}
