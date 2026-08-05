// Package harmony provides canonical Harmony adapters for isolated HotStuff
// integration work. Leader identities remain validator-level while the
// staking-era quorum roster preserves BLS-slot identities, exact decimal
// power, and canonical bitmaps. NewStakingQCAuthority binds both adapters to
// QC and timeout-certificate formation and verification. Protocol activation,
// networking, and canonical block writing remain outside this package.
package harmony
