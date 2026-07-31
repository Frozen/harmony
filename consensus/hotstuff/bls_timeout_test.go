package hotstuff

import (
	"testing"

	bls_core "github.com/harmony-one/harmony/crypto/bls/core"
	"github.com/stretchr/testify/require"
)

func TestBLSSignedTimeoutsFormVerifiableTCAndAdvancePacemaker(t *testing.T) {
	committee, secrets := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
		Member{ID: "dave", Power: 1},
	)
	domain := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	authority := NewQCAuthority(committee, domain)
	_, genesis, err := authority.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	pacemaker, err := authority.NewPacemaker(1)
	require.NoError(t, err)

	set := NewBLSTimeoutSet(authority, 1)
	for index, voter := range []MemberID{"carol", "alice", "bob"} {
		signed, err := SignTimeout(
			domain,
			Timeout{Voter: voter, View: 1, HighQC: genesis.QC()},
			secrets[voter],
		)
		require.NoError(t, err)
		require.NoError(t, set.Add(signed, BLSQC{QC: genesis.QC()}))
		if index == 1 {
			_, formed := set.Certificate()
			require.False(t, formed)
		}
	}

	certificate, formed := set.Certificate()
	require.True(t, formed)
	require.Equal(t, []MemberID{"alice", "bob", "carol"}, certificate.Signers)
	require.Equal(t, []byte{0b00000111}, certificate.Bitmap)
	verified, err := authority.VerifyTC(certificate)
	require.NoError(t, err)
	require.NoError(t, authority.AdvanceTimeout(pacemaker, verified))
	require.Equal(t, View(2), pacemaker.CurrentView())
	require.Equal(t, genesis.QC(), pacemaker.HighQC())
}

func TestBLSTimeoutRejectsForeignGenesisAndForgedSignature(t *testing.T) {
	committee, secrets := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	domain := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	authority := NewQCAuthority(committee, domain)
	_, genesis, err := authority.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)

	signed, err := SignTimeout(
		domain,
		Timeout{Voter: "alice", View: 1, HighQC: genesis.QC()},
		secrets["alice"],
	)
	require.NoError(t, err)
	forged := signed
	forged.Signature = append([]byte(nil), signed.Signature...)
	forged.Signature[0] ^= 0xff
	set := NewBLSTimeoutSet(authority, 1)
	require.ErrorIs(t, set.Add(forged, BLSQC{QC: genesis.QC()}), ErrInvalidTimeoutSignature)

	foreign := BLSQC{QC: QC{Block: "foreign-genesis", View: 0}}
	require.ErrorIs(t, set.Add(signed, foreign), ErrGenesisRootMismatch)
}

func TestBLSTimeoutCertificateRejectsTamperingAndWrongAuthority(t *testing.T) {
	committee, secrets := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	domain := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	authority := NewQCAuthority(committee, domain)
	_, genesis, err := authority.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	certificate := formBLSTC(t, authority, secrets, domain, 1, BLSQC{QC: genesis.QC()})

	badSignature := cloneBLSTC(certificate)
	badSignature.Signature[0] ^= 0xff
	_, err = authority.VerifyTC(badSignature)
	require.ErrorIs(t, err, ErrInvalidTCSignature)

	badBitmap := cloneBLSTC(certificate)
	badBitmap.Bitmap[0] ^= 1 << 3
	_, err = authority.VerifyTC(badBitmap)
	require.ErrorIs(t, err, ErrInvalidTCBitmap)

	reordered := cloneBLSTC(certificate)
	reordered.Signers[0], reordered.Signers[1] = reordered.Signers[1], reordered.Signers[0]
	_, err = authority.VerifyTC(reordered)
	require.ErrorIs(t, err, ErrNonCanonicalTCSigners)

	badReport := cloneBLSTC(certificate)
	badReport.Reports[0].Signature[0] ^= 0xff
	_, err = authority.VerifyTC(badReport)
	require.ErrorIs(t, err, ErrInvalidTimeoutHighQCSig)

	otherDomain := domain
	otherDomain.Epoch++
	otherAuthority := NewQCAuthority(committee, otherDomain)
	_, _, err = otherAuthority.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	_, err = otherAuthority.VerifyTC(certificate)
	require.ErrorIs(t, err, ErrInvalidTimeoutHighQCSig)

	verified, err := authority.VerifyTC(certificate)
	require.NoError(t, err)
	pacemaker, err := authority.NewPacemaker(1)
	require.NoError(t, err)
	certificate.Signers[0] = "mallory"
	certificate.HighQC.QC.Block = "mallory"
	require.NoError(t, authority.AdvanceTimeout(pacemaker, verified))
	require.Equal(t, View(2), pacemaker.CurrentView())
}

func TestBLSTimeoutCarriesVerifiedNonGenesisHighQC(t *testing.T) {
	committee, secrets := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	domain := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	authority := NewQCAuthority(committee, domain)
	_, _, err := authority.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	highQC := formBLSQC(t, committee, secrets, domain, "b1", 1)
	verifiedHighQC, err := authority.Verify(highQC)
	require.NoError(t, err)
	pacemaker, err := authority.NewPacemaker(1)
	require.NoError(t, err)
	require.NoError(t, authority.Advance(pacemaker, verifiedHighQC))

	certificate := formBLSTC(t, authority, secrets, domain, 2, highQC)
	verifiedTC, err := authority.VerifyTC(certificate)
	require.NoError(t, err)
	require.NoError(t, authority.AdvanceTimeout(pacemaker, verifiedTC))
	require.Equal(t, View(3), pacemaker.CurrentView())
	require.Equal(t, highQC.QC, pacemaker.HighQC())
}

func TestBLSTimeoutCertificateRejectsHighQCDowngrade(t *testing.T) {
	committee, secrets := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	domain := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	authority := NewQCAuthority(committee, domain)
	_, _, err := authority.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	lowerQC := formBLSQC(t, committee, secrets, domain, "b1", 1)
	higherQC := formBLSQC(t, committee, secrets, domain, "b2", 2)
	certificate := formBLSTC(t, authority, secrets, domain, 3, higherQC)

	certificate.HighQC = lowerQC
	_, err = authority.VerifyTC(certificate)
	require.ErrorIs(t, err, ErrTimeoutHighQCMismatch)
}

func TestBLSTimeoutCertificateRejectsConflictingSameViewQCs(t *testing.T) {
	committee, secrets := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	domain := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	authority := NewQCAuthority(committee, domain)
	_, _, err := authority.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	left := formBLSQC(t, committee, secrets, domain, "left", 1)
	right := formBLSQC(t, committee, secrets, domain, "right", 1)
	set := NewBLSTimeoutSet(authority, 2)

	for voter, highQC := range map[MemberID]BLSQC{
		"alice": left,
		"bob":   right,
		"carol": left,
	} {
		signed, err := SignTimeout(
			domain,
			Timeout{Voter: voter, View: 2, HighQC: highQC.QC},
			secrets[voter],
		)
		require.NoError(t, err)
		require.NoError(t, set.Add(signed, highQC))
	}
	certificate, formed := set.Certificate()
	require.True(t, formed)
	_, err = authority.VerifyTC(certificate)
	require.ErrorIs(t, err, ErrConflictingHighQC)
}

func TestBLSTimeoutCertificateRejectsNonAdjacentSameViewConflict(t *testing.T) {
	committee, secrets := testBLSCommittee(t,
		Member{ID: "alice", Power: 1},
		Member{ID: "bob", Power: 1},
		Member{ID: "carol", Power: 1},
	)
	domain := VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	authority := NewQCAuthority(committee, domain)
	_, _, err := authority.NewCore(Block{ID: "genesis", View: 0})
	require.NoError(t, err)
	lowLeft := formBLSQC(t, committee, secrets, domain, "low-left", 1)
	high := formBLSQC(t, committee, secrets, domain, "high", 2)
	lowRight := formBLSQC(t, committee, secrets, domain, "low-right", 1)
	set := NewBLSTimeoutSet(authority, 3)

	for voter, highQC := range map[MemberID]BLSQC{
		"alice": lowLeft,
		"bob":   high,
		"carol": lowRight,
	} {
		signed, err := SignTimeout(
			domain,
			Timeout{Voter: voter, View: 3, HighQC: highQC.QC},
			secrets[voter],
		)
		require.NoError(t, err)
		require.NoError(t, set.Add(signed, highQC))
	}
	certificate, formed := set.Certificate()
	require.True(t, formed)
	_, err = authority.VerifyTC(certificate)
	require.ErrorIs(t, err, ErrConflictingHighQC)
}

func formBLSTC(
	t *testing.T,
	authority *QCAuthority,
	secrets map[MemberID]*bls_core.SecretKey,
	domain VoteDomain,
	view View,
	highQC BLSQC,
) BLSTimeoutCertificate {
	t.Helper()
	set := NewBLSTimeoutSet(authority, view)
	for _, voter := range []MemberID{"carol", "alice", "bob"} {
		signed, err := SignTimeout(
			domain,
			Timeout{Voter: voter, View: view, HighQC: highQC.QC},
			secrets[voter],
		)
		require.NoError(t, err)
		require.NoError(t, set.Add(signed, highQC))
	}
	certificate, formed := set.Certificate()
	require.True(t, formed)
	return certificate
}

func cloneBLSTC(certificate BLSTimeoutCertificate) BLSTimeoutCertificate {
	reports := make([]BLSTimeoutReport, 0, len(certificate.Reports))
	for _, report := range certificate.Reports {
		reports = append(reports, cloneBLSTimeoutReport(report))
	}
	return BLSTimeoutCertificate{
		View:      certificate.View,
		HighQC:    cloneBLSQCEvidence(certificate.HighQC),
		Signers:   append([]MemberID(nil), certificate.Signers...),
		Reports:   reports,
		Signature: append([]byte(nil), certificate.Signature...),
		Bitmap:    append([]byte(nil), certificate.Bitmap...),
	}
}
