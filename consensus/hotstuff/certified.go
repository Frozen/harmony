package hotstuff

import (
	"errors"
	"sync"
)

var (
	ErrCertifiedQCMismatch = errors.New("hotstuff verified QC does not match the proposal justify")
	ErrWrongQCAuthority    = errors.New("hotstuff object belongs to a different committee or domain authority")
	ErrGenesisRootMismatch = errors.New("hotstuff authority is already bound to a different genesis root")
	ErrMissingGenesisRoot  = errors.New("hotstuff authority has no configured genesis root")
	ErrInvalidInitialView  = errors.New("hotstuff initial view must be after the genesis view")
	ErrNilBLSCommittee     = errors.New("hotstuff BLS committee is nil")
)

// QCAuthority binds certificate verification and structural state machines to
// one committee and vote domain. A node creates one authority for its active
// epoch and shard, then constructs its Core and Pacemaker through that object.
type QCAuthority struct {
	committee  *BLSCommittee
	leaders    LeaderSchedule
	domain     VoteDomain
	mu         sync.Mutex
	genesis    QC
	configured bool
}

func NewQCAuthority(committee *BLSCommittee, domain VoteDomain) *QCAuthority {
	return &QCAuthority{committee: committee, domain: domain}
}

// NewQCAuthorityWithLeaderSchedule separates validator-level leader rotation
// from the BLS-slot committee used for QC and TC verification.
func NewQCAuthorityWithLeaderSchedule(
	committee *BLSCommittee,
	domain VoteDomain,
	leaders LeaderSchedule,
) (*QCAuthority, error) {
	if committee == nil {
		return nil, ErrNilBLSCommittee
	}
	if isNilInterface(leaders) {
		return nil, ErrNilLeaderSchedule
	}
	return &QCAuthority{committee: committee, leaders: leaders, domain: domain}, nil
}

// NewVoteSet constructs a vote collector bound to this authority's committee
// and domain.
func (a *QCAuthority) NewVoteSet(block BlockID, view View) *BLSVoteSet {
	return NewBLSVoteSet(a.committee, block, view, a.domain)
}

// VerifiedQC is an immutable capability issued by one QCAuthority after BLS
// verification or for that authority's configured genesis trust root.
type VerifiedQC struct {
	qc        QC
	authority *QCAuthority
}

// QC returns an owned copy of the verified structural certificate.
func (v VerifiedQC) QC() QC {
	return cloneQC(v.qc)
}

// Verify validates aggregate BLS evidence in this authority's committee and
// domain, then mints the capability accepted by its state machines.
func (a *QCAuthority) Verify(certificate BLSQC) (VerifiedQC, error) {
	if err := a.committee.VerifyQC(a.domain, certificate); err != nil {
		return VerifiedQC{}, err
	}
	return VerifiedQC{qc: cloneQC(certificate.QC), authority: a}, nil
}

// NewCore validates and binds a genesis trust root to this authority. Returning
// the genesis capability here prevents another authority from minting one for
// an already configured Core.
func (a *QCAuthority) NewCore(genesis Block) (*Core, VerifiedQC, error) {
	core := newCore(genesis)
	if len(core.blocks) == 0 {
		return nil, VerifiedQC{}, ErrInvalidGenesis
	}
	if a.domain.Genesis == "" || genesis.ID != a.domain.Genesis {
		return nil, VerifiedQC{}, ErrGenesisRootMismatch
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	genesisQC := QC{Block: genesis.ID, View: genesis.View}
	if a.configured && !sameStructuralQC(a.genesis, genesisQC) {
		return nil, VerifiedQC{}, ErrGenesisRootMismatch
	}
	if !a.configured {
		a.genesis = cloneQC(genesisQC)
		a.configured = true
	}
	core.authority = a
	verifiedGenesis := VerifiedQC{
		qc:        cloneQC(a.genesis),
		authority: a,
	}
	return core, verifiedGenesis, nil
}

// NewPacemaker binds all successful-view transitions to this authority. The
// structural timeout transition remains private until timeout messages carry
// verifiable BLS evidence as well.
func (a *QCAuthority) NewPacemaker(initial View) (*Pacemaker, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.configured {
		return nil, ErrMissingGenesisRoot
	}
	if initial <= a.genesis.View {
		return nil, ErrInvalidInitialView
	}
	leaders := a.leaders
	if leaders == nil {
		leaders = a.committee.committee
	}
	pacemaker := newPacemakerWithLeaderSchedule(a.committee.committee, leaders, initial)
	pacemaker.authority = a
	pacemaker.highQC = cloneQC(a.genesis)
	return pacemaker, nil
}

// Accept is the certificate-gated proposal ingress. The crypto-independent
// Core keeps accept private so network adapters cannot submit structural QCs.
func (a *QCAuthority) Accept(core *Core, block Block, justify VerifiedQC) ([]BlockID, error) {
	block = cloneBlock(block)
	if core.authority != a || justify.authority != a {
		return nil, ErrWrongQCAuthority
	}
	if !sameStructuralQC(block.Justify, justify.qc) {
		return nil, ErrCertifiedQCMismatch
	}
	return core.accept(block)
}

// Advance is the certificate-gated successful-view transition.
func (a *QCAuthority) Advance(pacemaker *Pacemaker, qc VerifiedQC) error {
	if pacemaker.authority != a || qc.authority != a {
		return ErrWrongQCAuthority
	}
	return pacemaker.advanceQC(qc.qc)
}

func sameStructuralQC(left, right QC) bool {
	return left.Block == right.Block &&
		left.View == right.View &&
		equalMemberIDs(left.Signers, right.Signers)
}
