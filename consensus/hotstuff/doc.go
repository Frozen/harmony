// Package hotstuff contains an isolated chained HotStuff research spike.
//
// It is not wired into Harmony consensus, networking, durable storage, block
// execution, wire formats, or production activation. The package currently
// validates leader rotation, safety rules, pacemaker timeouts, broadcast vote
// aggregation with Harmony BLS signatures, authority-bound verified QCs,
// structural block processing, and the direct three-chain commit rule.
package hotstuff
