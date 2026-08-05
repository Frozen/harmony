package hotstuff

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"sync"

	hmybls "github.com/harmony-one/harmony/crypto/bls"
	bls_core "github.com/harmony-one/harmony/crypto/bls/core"
)

const (
	hotStuffTimeoutDomain       = "harmony/hotstuff/timeout/v1"
	hotStuffTimeoutHighQCDomain = "harmony/hotstuff/timeout-high-qc/v1"
)

var (
	ErrInvalidTimeoutSignature = errors.New("hotstuff timeout has an invalid BLS signature")
	ErrInvalidTimeoutHighQCSig = errors.New("hotstuff timeout HighQC binding has an invalid BLS signature")
	ErrInvalidTCSignature      = errors.New("hotstuff TC has an invalid aggregate BLS signature")
	ErrInvalidTCBitmap         = errors.New("hotstuff TC bitmap does not match its signers")
	ErrNonCanonicalTCSigners   = errors.New("hotstuff TC signers are not in canonical committee order")
	ErrTimeoutHighQCMismatch   = errors.New("hotstuff timeout HighQC does not match its cryptographic evidence")
	ErrInvalidTCReports        = errors.New("hotstuff TC reports do not match its signers")
	ErrConflictingHighQC       = errors.New("hotstuff TC reports conflicting QCs at the same view")
)

// SignedTimeout has a compact view-abandonment signature for aggregation and a
// second signature binding this sender to its independently certified HighQC.
type SignedTimeout struct {
	Timeout
	Signature       []byte
	HighQCSignature []byte
}

// BLSTimeoutReport preserves one signer's authenticated HighQC claim.
type BLSTimeoutReport struct {
	Voter     MemberID
	HighQC    BLSQC
	Signature []byte
}

// BLSTimeoutCertificate proves weighted abandonment of View and preserves the
// signed reports from which every verifier independently selects HighQC.
type BLSTimeoutCertificate struct {
	View      View
	HighQC    BLSQC
	Signers   []MemberID
	Reports   []BLSTimeoutReport
	Signature []byte
	Bitmap    []byte
}

// VerifiedTC is an opaque, authority-bound timeout transition capability.
type VerifiedTC struct {
	certificate TimeoutCertificate
	authority   *QCAuthority
}

func SignTimeout(domain VoteDomain, timeout Timeout, secret *bls_core.SecretKey) (SignedTimeout, error) {
	if secret == nil {
		return SignedTimeout{}, ErrNilBLSSecretKey
	}
	if timeout.HighQC.View > timeout.View {
		return SignedTimeout{}, ErrFutureQC
	}
	viewDigest, err := timeoutDigest(domain, timeout.View)
	if err != nil {
		return SignedTimeout{}, err
	}
	viewSignature := secret.SignHash(viewDigest[:])
	if viewSignature == nil {
		return SignedTimeout{}, ErrInvalidTimeoutSignature
	}
	highQCDigest, err := timeoutHighQCDigest(domain, timeout)
	if err != nil {
		return SignedTimeout{}, err
	}
	highQCSignature := secret.SignHash(highQCDigest[:])
	if highQCSignature == nil {
		return SignedTimeout{}, ErrInvalidTimeoutHighQCSig
	}
	return SignedTimeout{
		Timeout: Timeout{
			Voter:  timeout.Voter,
			View:   timeout.View,
			HighQC: cloneQC(timeout.HighQC),
		},
		Signature:       append([]byte(nil), viewSignature.Serialize()...),
		HighQCSignature: append([]byte(nil), highQCSignature.Serialize()...),
	}, nil
}

// BLSTimeoutSet verifies timeout signatures and carried QC evidence before
// admitting voting power to the structural timeout collector.
type BLSTimeoutSet struct {
	mu         sync.Mutex
	authority  *QCAuthority
	timeouts   *TimeoutSet
	signatures map[MemberID]*bls_core.Sign
	reports    map[MemberID]BLSTimeoutReport
	highQC     BLSQC
}

func NewBLSTimeoutSet(authority *QCAuthority, view View) *BLSTimeoutSet {
	return &BLSTimeoutSet{
		authority:  authority,
		timeouts:   NewTimeoutSet(authority.committee.committee, view),
		signatures: make(map[MemberID]*bls_core.Sign),
		reports:    make(map[MemberID]BLSTimeoutReport),
	}
}

func (s *BLSTimeoutSet) Add(timeout SignedTimeout, evidence BLSQC) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	verifiedHighQC, err := s.authority.verifyQCEvidence(evidence)
	if err != nil {
		return err
	}
	if !sameStructuralQC(timeout.HighQC, verifiedHighQC.qc) {
		return ErrTimeoutHighQCMismatch
	}
	member, exists := s.authority.committee.byID[timeout.Voter]
	if !exists {
		return ErrUnknownVoter
	}
	viewSignature, err := deserializeSignature(timeout.Signature, ErrInvalidTimeoutSignature)
	if err != nil {
		return err
	}
	viewDigest, err := timeoutDigest(s.authority.domain, timeout.View)
	if err != nil {
		return err
	}
	if !viewSignature.VerifyHash(member.PublicKey.Object, viewDigest[:]) {
		return ErrInvalidTimeoutSignature
	}
	highQCSignature, err := deserializeSignature(timeout.HighQCSignature, ErrInvalidTimeoutHighQCSig)
	if err != nil {
		return err
	}
	highQCDigest, err := timeoutHighQCDigest(s.authority.domain, timeout.Timeout)
	if err != nil {
		return err
	}
	if !highQCSignature.VerifyHash(member.PublicKey.Object, highQCDigest[:]) {
		return ErrInvalidTimeoutHighQCSig
	}
	if err := s.timeouts.Add(timeout.Timeout); err != nil {
		return err
	}
	s.signatures[timeout.Voter] = viewSignature
	s.reports[timeout.Voter] = BLSTimeoutReport{
		Voter:     timeout.Voter,
		HighQC:    cloneBLSQCEvidence(evidence),
		Signature: append([]byte(nil), timeout.HighQCSignature...),
	}
	if s.highQC.QC.Block == "" || higherQC(verifiedHighQC.qc, s.highQC.QC) {
		s.highQC = cloneBLSQCEvidence(evidence)
	}
	return nil
}

func (s *BLSTimeoutSet) Certificate() (BLSTimeoutCertificate, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	certificate, formed, err := s.timeouts.Certificate()
	if err != nil {
		return BLSTimeoutCertificate{}, false, err
	}
	if !formed {
		return BLSTimeoutCertificate{}, false, nil
	}
	signatures := make([]*bls_core.Sign, 0, len(certificate.Signers))
	reports := make([]BLSTimeoutReport, 0, len(certificate.Signers))
	mask := hmybls.NewMask(s.authority.committee.publics)
	for _, signer := range certificate.Signers {
		signatures = append(signatures, s.signatures[signer])
		reports = append(reports, cloneBLSTimeoutReport(s.reports[signer]))
		member := s.authority.committee.byID[signer]
		if err := mask.SetKey(member.PublicKey.Bytes, true); err != nil {
			return BLSTimeoutCertificate{}, false, err
		}
	}
	aggregate := hmybls.AggregateSig(signatures)
	return BLSTimeoutCertificate{
		View:      certificate.View,
		HighQC:    cloneBLSQCEvidence(s.highQC),
		Signers:   append([]MemberID(nil), certificate.Signers...),
		Reports:   reports,
		Signature: append([]byte(nil), aggregate.Serialize()...),
		Bitmap:    mask.Mask(),
	}, true, nil
}

func (a *QCAuthority) VerifyTC(certificate BLSTimeoutCertificate) (VerifiedTC, error) {
	if err := a.committee.committee.requireQuorum(certificate.Signers); err != nil {
		return VerifiedTC{}, err
	}
	canonical := a.committee.committee.canonicalQC(QC{Signers: certificate.Signers})
	if !equalMemberIDs(certificate.Signers, canonical.Signers) {
		return VerifiedTC{}, ErrNonCanonicalTCSigners
	}
	if len(certificate.Reports) != len(certificate.Signers) {
		return VerifiedTC{}, ErrInvalidTCReports
	}

	mask := hmybls.NewMask(a.committee.publics)
	var selected BLSQC
	blocksByView := make(map[View]BlockID, len(certificate.Reports))
	for index, signer := range certificate.Signers {
		member := a.committee.byID[signer]
		if err := mask.SetKey(member.PublicKey.Bytes, true); err != nil {
			return VerifiedTC{}, ErrInvalidTCBitmap
		}
		report := certificate.Reports[index]
		if report.Voter != signer {
			return VerifiedTC{}, ErrInvalidTCReports
		}
		verifiedHighQC, err := a.verifyQCEvidence(report.HighQC)
		if err != nil {
			return VerifiedTC{}, err
		}
		if verifiedHighQC.qc.View > certificate.View {
			return VerifiedTC{}, ErrFutureQC
		}
		reportSignature, err := deserializeSignature(report.Signature, ErrInvalidTimeoutHighQCSig)
		if err != nil {
			return VerifiedTC{}, err
		}
		reportDigest, err := timeoutHighQCDigest(a.domain, Timeout{
			Voter: signer, View: certificate.View, HighQC: verifiedHighQC.qc,
		})
		if err != nil {
			return VerifiedTC{}, err
		}
		if !reportSignature.VerifyHash(member.PublicKey.Object, reportDigest[:]) {
			return VerifiedTC{}, ErrInvalidTimeoutHighQCSig
		}
		if block, exists := blocksByView[verifiedHighQC.qc.View]; exists &&
			block != verifiedHighQC.qc.Block {
			return VerifiedTC{}, ErrConflictingHighQC
		}
		blocksByView[verifiedHighQC.qc.View] = verifiedHighQC.qc.Block
		if selected.QC.Block == "" || higherQC(verifiedHighQC.qc, selected.QC) {
			selected = cloneBLSQCEvidence(report.HighQC)
		}
	}
	if !bytes.Equal(mask.Mask(), certificate.Bitmap) {
		return VerifiedTC{}, ErrInvalidTCBitmap
	}
	viewSignature, err := deserializeSignature(certificate.Signature, ErrInvalidTCSignature)
	if err != nil {
		return VerifiedTC{}, err
	}
	viewDigest, err := timeoutDigest(a.domain, certificate.View)
	if err != nil {
		return VerifiedTC{}, err
	}
	if !viewSignature.VerifyHash(mask.AggregatePublic, viewDigest[:]) {
		return VerifiedTC{}, ErrInvalidTCSignature
	}
	if !sameStructuralQC(certificate.HighQC.QC, selected.QC) {
		return VerifiedTC{}, ErrTimeoutHighQCMismatch
	}
	selectedVerified, err := a.verifyQCEvidence(selected)
	if err != nil {
		return VerifiedTC{}, err
	}
	return VerifiedTC{
		certificate: TimeoutCertificate{
			View:    certificate.View,
			HighQC:  cloneQC(selectedVerified.qc),
			Signers: append([]MemberID(nil), certificate.Signers...),
		},
		authority: a,
	}, nil
}

func (a *QCAuthority) AdvanceTimeout(pacemaker *Pacemaker, certificate VerifiedTC) error {
	if pacemaker.authority != a || certificate.authority != a {
		return ErrWrongQCAuthority
	}
	return pacemaker.advanceTimeout(certificate.certificate)
}

func (a *QCAuthority) verifyQCEvidence(evidence BLSQC) (VerifiedQC, error) {
	a.mu.Lock()
	if !a.configured {
		a.mu.Unlock()
		return VerifiedQC{}, ErrMissingGenesisRoot
	}
	genesis := cloneQC(a.genesis)
	a.mu.Unlock()

	if evidence.QC.View == 0 {
		if !sameStructuralQC(evidence.QC, genesis) || len(evidence.Signature) != 0 || len(evidence.Bitmap) != 0 {
			return VerifiedQC{}, ErrGenesisRootMismatch
		}
		return VerifiedQC{qc: genesis, authority: a}, nil
	}
	return a.Verify(evidence)
}

func cloneBLSQCEvidence(evidence BLSQC) BLSQC {
	return BLSQC{
		QC:        cloneQC(evidence.QC),
		Signature: append([]byte(nil), evidence.Signature...),
		Bitmap:    append([]byte(nil), evidence.Bitmap...),
	}
}

func cloneBLSTimeoutReport(report BLSTimeoutReport) BLSTimeoutReport {
	return BLSTimeoutReport{
		Voter:     report.Voter,
		HighQC:    cloneBLSQCEvidence(report.HighQC),
		Signature: append([]byte(nil), report.Signature...),
	}
}

func timeoutDigest(domain VoteDomain, view View) ([sha256.Size]byte, error) {
	return timeoutDomainDigest(hotStuffTimeoutDomain, domain, view, nil)
}

func timeoutHighQCDigest(domain VoteDomain, timeout Timeout) ([sha256.Size]byte, error) {
	if uint64(len(timeout.Voter)) > uint64(math.MaxUint32) ||
		uint64(len(timeout.HighQC.Block)) > uint64(math.MaxUint32) ||
		uint64(len(timeout.HighQC.Signers)) > uint64(math.MaxUint32) {
		return [sha256.Size]byte{}, ErrBlockIDTooLong
	}
	extra := make([]byte, 0)
	extra = appendUint32String(extra, string(timeout.Voter))
	var fixed [8]byte
	binary.BigEndian.PutUint64(fixed[:], uint64(timeout.HighQC.View))
	extra = append(extra, fixed[:]...)
	extra = appendUint32String(extra, string(timeout.HighQC.Block))
	binary.BigEndian.PutUint32(fixed[:4], uint32(len(timeout.HighQC.Signers)))
	extra = append(extra, fixed[:4]...)
	for _, signer := range timeout.HighQC.Signers {
		if uint64(len(signer)) > uint64(math.MaxUint32) {
			return [sha256.Size]byte{}, ErrBlockIDTooLong
		}
		extra = appendUint32String(extra, string(signer))
	}
	return timeoutDomainDigest(hotStuffTimeoutHighQCDomain, domain, timeout.View, extra)
}

func timeoutDomainDigest(tag string, domain VoteDomain, view View, extra []byte) ([sha256.Size]byte, error) {
	if domain.Genesis == "" {
		return [sha256.Size]byte{}, ErrMissingGenesisRoot
	}
	if uint64(len(domain.Genesis)) > uint64(math.MaxUint32) {
		return [sha256.Size]byte{}, ErrBlockIDTooLong
	}
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(tag))
	var fixed [8]byte
	binary.BigEndian.PutUint32(fixed[:4], domain.ChainID)
	_, _ = hasher.Write(fixed[:4])
	binary.BigEndian.PutUint32(fixed[:4], domain.ShardID)
	_, _ = hasher.Write(fixed[:4])
	binary.BigEndian.PutUint64(fixed[:], domain.Epoch)
	_, _ = hasher.Write(fixed[:])
	binary.BigEndian.PutUint32(fixed[:4], uint32(len(domain.Genesis)))
	_, _ = hasher.Write(fixed[:4])
	_, _ = hasher.Write([]byte(domain.Genesis))
	binary.BigEndian.PutUint64(fixed[:], uint64(view))
	_, _ = hasher.Write(fixed[:])
	_, _ = hasher.Write(extra)

	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest, nil
}

func appendUint32String(target []byte, value string) []byte {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	target = append(target, length[:]...)
	return append(target, value...)
}
