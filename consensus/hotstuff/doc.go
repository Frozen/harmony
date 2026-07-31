// Package hotstuff contains an isolated chained HotStuff research spike.
//
// It is not wired into Harmony consensus, networking, persistence, block
// verification, or BLS signatures. The package currently validates leader
// rotation, broadcast vote aggregation, structural QCs, and the direct
// three-chain commit rule.
package hotstuff
