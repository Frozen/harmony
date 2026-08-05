package hotstuff

import (
	"errors"
	"math"
	"sync"
)

var (
	ErrDuplicateTimeout        = errors.New("hotstuff voter already timed out this view")
	ErrWrongTimeoutView        = errors.New("hotstuff timeout targets a different view")
	ErrFutureQC                = errors.New("hotstuff timeout carries a QC from a future view")
	ErrStaleCertificate        = errors.New("hotstuff certificate does not advance the current view")
	ErrWrongCertificateView    = errors.New("hotstuff timeout certificate does not target the current view")
	ErrInsufficientVotingPower = errors.New("hotstuff certificate has insufficient voting power")
	ErrDuplicateSigner         = errors.New("hotstuff certificate contains a duplicate signer")
	ErrInvalidQC               = errors.New("hotstuff QC has no certified block")
	ErrViewOverflow            = errors.New("hotstuff view overflows uint64")
)

// Timeout is broadcast when a replica cannot make progress in a view. HighQC
// lets every prospective next leader learn the safest certified branch.
type Timeout struct {
	Voter  MemberID
	View   View
	HighQC QC
}

// TimeoutCertificate proves that a weighted quorum abandoned one view.
type TimeoutCertificate struct {
	View    View
	HighQC  QC
	Signers []MemberID
}

// TimeoutSet collects broadcast timeout messages for one view.
type TimeoutSet struct {
	mu        sync.Mutex
	committee *Committee
	view      View
	voters    map[MemberID]struct{}
	highQC    QC
}

func NewTimeoutSet(committee *Committee, view View) *TimeoutSet {
	return &TimeoutSet{
		committee: committee,
		view:      view,
		voters:    make(map[MemberID]struct{}),
	}
}

func (s *TimeoutSet) Add(timeout Timeout) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.committee.byID[timeout.Voter]
	if !exists {
		return ErrUnknownVoter
	}
	if timeout.View != s.view {
		return ErrWrongTimeoutView
	}
	if timeout.HighQC.View > timeout.View {
		return ErrFutureQC
	}
	if err := s.committee.requireQC(timeout.HighQC); err != nil {
		return err
	}
	if _, exists := s.voters[timeout.Voter]; exists {
		return ErrDuplicateTimeout
	}

	s.voters[timeout.Voter] = struct{}{}
	canonicalHighQC := s.committee.canonicalQC(timeout.HighQC)
	if higherQC(canonicalHighQC, s.highQC) {
		s.highQC = canonicalHighQC
	}
	return nil
}

func (s *TimeoutSet) Certificate() (TimeoutCertificate, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	signers := make([]MemberID, 0, len(s.voters))
	for _, member := range s.committee.members {
		if _, exists := s.voters[member.ID]; exists {
			signers = append(signers, member.ID)
		}
	}
	hasQuorum, err := s.committee.hasQuorum(signers)
	if err != nil {
		return TimeoutCertificate{}, false, err
	}
	if !hasQuorum {
		return TimeoutCertificate{}, false, nil
	}
	return TimeoutCertificate{
		View:    s.view,
		HighQC:  cloneQC(s.highQC),
		Signers: signers,
	}, true, nil
}

// Pacemaker advances views after either a successful QC or a timeout
// certificate. Advancing a timed-out view does not require producing a block.
type Pacemaker struct {
	mu        sync.Mutex
	committee *Committee
	leaders   LeaderSchedule
	view      View
	highQC    QC
	authority *QCAuthority
}

func newPacemaker(committee *Committee, initial View) *Pacemaker {
	return newPacemakerWithLeaderSchedule(committee, committee, initial)
}

func newPacemakerWithLeaderSchedule(
	committee *Committee,
	leaders LeaderSchedule,
	initial View,
) *Pacemaker {
	return &Pacemaker{committee: committee, leaders: leaders, view: initial}
}

func (p *Pacemaker) CurrentView() View {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.view
}

func (p *Pacemaker) Leader() MemberID {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.leaders.Leader(p.view)
}

func (p *Pacemaker) HighQC() QC {
	p.mu.Lock()
	defer p.mu.Unlock()
	return cloneQC(p.highQC)
}

func (p *Pacemaker) advanceTimeout(certificate TimeoutCertificate) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if certificate.View < p.view {
		return ErrStaleCertificate
	}
	if certificate.View > p.view {
		return ErrWrongCertificateView
	}
	if certificate.HighQC.View > certificate.View {
		return ErrFutureQC
	}
	if err := p.committee.requireQuorum(certificate.Signers); err != nil {
		return err
	}
	if err := p.committee.requireQC(certificate.HighQC); err != nil {
		return err
	}
	if err := p.advanceAfter(certificate.View); err != nil {
		return err
	}
	if higherQC(certificate.HighQC, p.highQC) {
		p.highQC = p.committee.canonicalQC(certificate.HighQC)
	}
	return nil
}

func (p *Pacemaker) advanceQC(qc QC) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if qc.View < p.view {
		return ErrStaleCertificate
	}
	if err := p.committee.requireQC(qc); err != nil {
		return err
	}
	if err := p.advanceAfter(qc.View); err != nil {
		return err
	}
	if higherQC(qc, p.highQC) {
		p.highQC = p.committee.canonicalQC(qc)
	}
	return nil
}

func (p *Pacemaker) advanceAfter(certifiedView View) error {
	if certifiedView == View(math.MaxUint64) {
		return ErrViewOverflow
	}
	next := certifiedView + 1
	if next <= p.view {
		return ErrStaleCertificate
	}
	p.view = next
	return nil
}

func (c *Committee) requireQuorum(signers []MemberID) error {
	hasQuorum, err := c.hasQuorum(signers)
	if err != nil {
		return err
	}
	if !hasQuorum {
		return ErrInsufficientVotingPower
	}
	return nil
}

func (c *Committee) hasQuorum(signers []MemberID) (bool, error) {
	seen := make(map[MemberID]struct{}, len(signers))
	var power uint64
	for _, signer := range signers {
		if _, exists := seen[signer]; exists {
			return false, ErrDuplicateSigner
		}
		member, exists := c.byID[signer]
		if !exists {
			return false, ErrUnknownVoter
		}
		seen[signer] = struct{}{}
		power += member.Power
	}
	if c.certificateQuorum != nil {
		hasQuorum, err := c.certificateQuorum.HasQuorum(append([]MemberID(nil), signers...))
		if err != nil {
			return false, err
		}
		return hasQuorum, nil
	}
	return power >= c.quorumPower(), nil
}

func (c *Committee) requireQC(qc QC) error {
	// View zero is the externally configured trust root and does not require a
	// quorum witness. Every later QC must certify a concrete block.
	if qc.View == 0 && len(qc.Signers) == 0 {
		return nil
	}
	if qc.Block == "" {
		return ErrInvalidQC
	}
	return c.requireQuorum(qc.Signers)
}

func (c *Committee) canonicalQC(qc QC) QC {
	signerSet := make(map[MemberID]struct{}, len(qc.Signers))
	for _, signer := range qc.Signers {
		signerSet[signer] = struct{}{}
	}
	signers := make([]MemberID, 0, len(signerSet))
	for _, member := range c.members {
		if _, exists := signerSet[member.ID]; exists {
			signers = append(signers, member.ID)
		}
	}
	return QC{Block: qc.Block, View: qc.View, Signers: signers}
}

func higherQC(left, right QC) bool {
	// Signer lists are proof witnesses, not part of the logical QC ordering.
	return left.View > right.View ||
		(left.View == right.View && left.Block > right.Block)
}
