package harmony

import (
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/harmony-one/harmony/consensus/hotstuff"
	hmybls "github.com/harmony-one/harmony/crypto/bls"
	"github.com/stretchr/testify/require"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

func TestLevelDBSafetyStoreDatabaseUsesSyncWrites(t *testing.T) {
	db := &recordingLevelDB{}
	backend := newLevelDBSafetyStoreDatabase(db)

	require.NoError(t, backend.updateSync([]byte("key"), []byte("value"), nil))
	require.NotNil(t, db.writeOptions)
	require.True(t, db.writeOptions.Sync)
}

func TestOpenLevelDBSafetyStoreDatabase(t *testing.T) {
	db, err := OpenLevelDBSafetyStoreDatabase(filepath.Join(t.TempDir(), "safety"))
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

func TestCopiedLevelDBSafetyStoreDatabaseSharesCloseLifecycle(t *testing.T) {
	db, err := OpenLevelDBSafetyStoreDatabase(filepath.Join(t.TempDir(), "safety"))
	require.NoError(t, err)
	copy := *db

	require.NoError(t, db.Close())
	require.NoError(t, copy.Close())
	_, err = copy.Has([]byte("key"))
	require.Error(t, err)
}

func TestSafetyStorePersistsThroughSynchronousBackend(t *testing.T) {
	db := &recordingLevelDB{}
	backend := newLevelDBSafetyStoreDatabase(db)
	domain := hotstuff.VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	key := hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey())
	store, err := NewSafetyStore(backend, domain, key.Pub.Bytes)
	require.NoError(t, err)

	require.NoError(t, store.Save(hotstuff.SafetyState{LastVotedView: 1}))
	require.NotNil(t, db.writeOptions)
	require.True(t, db.writeOptions.Sync)
}

func TestSafetyStoreRejectsTypedNilDatabase(t *testing.T) {
	var db *LevelDBSafetyStoreDatabase
	domain := hotstuff.VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	key := hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey())

	store, err := NewSafetyStore(db, domain, key.Pub.Bytes)
	require.Nil(t, store)
	require.ErrorIs(t, err, ErrNilSafetyStoreDatabase)
}

func TestSafetyStoreRejectsZeroValueDatabase(t *testing.T) {
	db := &LevelDBSafetyStoreDatabase{}
	domain := hotstuff.VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	key := hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey())

	store, err := NewSafetyStore(db, domain, key.Pub.Bytes)
	require.Nil(t, store)
	require.ErrorIs(t, err, ErrNilSafetyStoreDatabase)
}

func TestSafetyStoreRejectsConflictingMultiOwnerVote(t *testing.T) {
	db := newTestSafetyDatabase(t)
	domain := hotstuff.VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	key := hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey())
	firstStore, err := NewSafetyStore(db, domain, key.Pub.Bytes)
	require.NoError(t, err)
	secondStore, err := NewSafetyStore(db, domain, key.Pub.Bytes)
	require.NoError(t, err)

	_, firstAuthority, firstCore, firstGenesis := safetyAuthority(t, domain, *key.Pub)
	firstBlock := hotstuff.Block{ID: "first", Parent: domain.Genesis, View: 1, Justify: firstGenesis.QC()}
	_, err = firstAuthority.Accept(firstCore, firstBlock, firstGenesis)
	require.NoError(t, err)
	firstRules := hotstuff.NewSafetyRules(
		firstCore,
		hotstuff.SafetyState{LockedQC: firstGenesis.QC()},
		firstStore.Save,
	)

	_, secondAuthority, secondCore, secondGenesis := safetyAuthority(t, domain, *key.Pub)
	secondBlock := hotstuff.Block{ID: "second", Parent: domain.Genesis, View: 1, Justify: secondGenesis.QC()}
	_, err = secondAuthority.Accept(secondCore, secondBlock, secondGenesis)
	require.NoError(t, err)
	secondInitial := hotstuff.SafetyState{LockedQC: secondGenesis.QC()}
	secondRules := hotstuff.NewSafetyRules(secondCore, secondInitial, secondStore.Save)

	_, err = firstRules.Vote("validator-a", firstBlock)
	require.NoError(t, err)
	_, err = secondRules.Vote("validator-a", secondBlock)
	require.ErrorIs(t, err, ErrSafetyStoreStateRegression)
	require.Equal(t, secondInitial, secondRules.State())
}

func TestCopiedLevelDBSafetyStoreDatabaseRejectsSameViewDoubleWrite(t *testing.T) {
	rawDB := newCoordinatedLevelDB()
	firstDB := newLevelDBSafetyStoreDatabase(rawDB)
	secondDB := *firstDB
	domain := hotstuff.VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	key := hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey())
	first, err := NewSafetyStore(firstDB, domain, key.Pub.Bytes)
	require.NoError(t, err)
	second, err := NewSafetyStore(&secondDB, domain, key.Pub.Bytes)
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, save := range []func(hotstuff.SafetyState) error{first.Save, second.Save} {
		go func(save func(hotstuff.SafetyState) error) {
			<-start
			results <- save(hotstuff.SafetyState{
				LastVotedView: 1,
				LockedQC:      hotstuff.QC{Block: domain.Genesis, View: 0},
			})
		}(save)
	}
	close(start)

	var succeeded, rejected int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrSafetyStoreStateRegression):
			rejected++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, rejected)
}

func TestSafetyStoreAllowsMonotonicTransition(t *testing.T) {
	db := newTestSafetyDatabase(t)
	domain := hotstuff.VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	key := hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey())
	store, err := NewSafetyStore(db, domain, key.Pub.Bytes)
	require.NoError(t, err)

	require.NoError(t, store.Save(hotstuff.SafetyState{
		LastVotedView: 1,
		LockedQC:      hotstuff.QC{Block: domain.Genesis, View: 0},
	}))
	want := hotstuff.SafetyState{
		LastVotedView: 2,
		LockedQC:      hotstuff.QC{Block: "b1", View: 1},
	}
	require.NoError(t, store.Save(want))

	got, found, err := store.Load()
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, want, got)
}

func TestSafetyStoreRejectsNonMonotonicTransition(t *testing.T) {
	initial := hotstuff.SafetyState{
		LastVotedView: 2,
		LockedQC:      hotstuff.QC{Block: "b1", View: 1},
	}
	tests := []struct {
		name      string
		candidate hotstuff.SafetyState
	}{
		{name: "equal vote view", candidate: initial},
		{name: "regressing vote view", candidate: hotstuff.SafetyState{
			LastVotedView: 1, LockedQC: hotstuff.QC{Block: "b1", View: 1},
		}},
		{name: "regressing lock view", candidate: hotstuff.SafetyState{
			LastVotedView: 3, LockedQC: hotstuff.QC{Block: "genesis", View: 0},
		}},
		{name: "conflicting lock block", candidate: hotstuff.SafetyState{
			LastVotedView: 3, LockedQC: hotstuff.QC{Block: "fork", View: 1},
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestSafetyDatabase(t)
			domain := hotstuff.VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
			key := hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey())
			store, err := NewSafetyStore(db, domain, key.Pub.Bytes)
			require.NoError(t, err)
			require.NoError(t, store.Save(initial))

			err = store.Save(test.candidate)
			require.ErrorIs(t, err, ErrSafetyStoreStateRegression)
			got, found, err := store.Load()
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, initial, got)
		})
	}
}

func TestSafetyStoreSurvivesAbruptProcessExit(t *testing.T) {
	const helperEnvironment = "HARMONY_HOTSTUFF_SAFETY_CRASH_HELPER"
	path := filepath.Join(t.TempDir(), "crash-db")
	domain := hotstuff.VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	var signer hmybls.SerializedPublicKey
	signer[0] = 1
	want := hotstuff.SafetyState{
		LastVotedView: 7,
		LockedQC:      hotstuff.QC{Block: "locked", View: 6},
	}

	if os.Getenv(helperEnvironment) == "1" {
		db, err := OpenLevelDBSafetyStoreDatabase(os.Getenv("HARMONY_HOTSTUFF_SAFETY_CRASH_PATH"))
		if err != nil {
			os.Exit(2)
		}
		store, err := NewSafetyStore(db, domain, signer)
		if err != nil {
			os.Exit(3)
		}
		if err := store.Save(want); err != nil {
			os.Exit(4)
		}
		os.Exit(0)
	}

	command := exec.Command(os.Args[0], "-test.run=^TestSafetyStoreSurvivesAbruptProcessExit$")
	command.Env = append(os.Environ(),
		helperEnvironment+"=1",
		"HARMONY_HOTSTUFF_SAFETY_CRASH_PATH="+path,
	)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))

	db := openTestSafetyDatabase(t, path)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	store, err := NewSafetyStore(db, domain, signer)
	require.NoError(t, err)
	got, found, err := store.Load()
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, want, got)
}

func TestSafetyStoreSurvivesLevelDBReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chaindata")
	domain := hotstuff.VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	key := hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey())

	db := openTestSafetyDatabase(t, path)
	store, err := NewSafetyStore(db, domain, key.Pub.Bytes)
	require.NoError(t, err)
	core, genesis, proposal := safetyRuntime(t, domain, *key.Pub)
	rules := hotstuff.NewSafetyRules(core, hotstuff.SafetyState{LockedQC: genesis.QC()}, store.Save)

	_, err = rules.Vote("validator-a", proposal)
	require.NoError(t, err)
	persisted := rules.State()
	require.NoError(t, db.Close())

	db = openTestSafetyDatabase(t, path)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	store, err = NewSafetyStore(db, domain, key.Pub.Bytes)
	require.NoError(t, err)
	restored, found, err := store.Load()
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, persisted, restored)

	core, _, proposal = safetyRuntime(t, domain, *key.Pub)
	restarted := hotstuff.NewSafetyRules(core, restored, store.Save)
	_, err = restarted.Vote("validator-a", proposal)
	require.ErrorIs(t, err, hotstuff.ErrAlreadyVoted)
}

func TestSafetyStoreRestoresLockedQCAcrossLevelDBReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chaindata")
	domain := hotstuff.VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	key := hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey())
	db := openTestSafetyDatabase(t, path)
	store, err := NewSafetyStore(db, domain, key.Pub.Bytes)
	require.NoError(t, err)
	committee, authority, core, genesis := safetyAuthority(t, domain, *key.Pub)
	b1 := hotstuff.Block{ID: "b1", Parent: domain.Genesis, View: 1, Justify: genesis.QC()}
	qc1 := acceptAndCertify(t, committee, authority, core, domain, key, b1, genesis)
	b2 := hotstuff.Block{ID: "b2", Parent: b1.ID, View: 2, Justify: qc1.QC()}
	qc2 := acceptAndCertify(t, committee, authority, core, domain, key, b2, qc1)
	b3 := hotstuff.Block{ID: "b3", Parent: b2.ID, View: 3, Justify: qc2.QC()}
	_, err = authority.Accept(core, b3, qc2)
	require.NoError(t, err)
	rules := hotstuff.NewSafetyRules(core, hotstuff.SafetyState{LockedQC: genesis.QC()}, store.Save)
	_, err = rules.Vote("validator-a", b3)
	require.NoError(t, err)
	require.Equal(t, qc1.QC(), rules.State().LockedQC)
	require.NoError(t, db.Close())

	db = openTestSafetyDatabase(t, path)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	store, err = NewSafetyStore(db, domain, key.Pub.Bytes)
	require.NoError(t, err)
	restored, found, err := store.Load()
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, qc1.QC(), restored.LockedQC)

	committee, authority, core, genesis = safetyAuthority(t, domain, *key.Pub)
	b1.Justify = genesis.QC()
	qc1 = acceptAndCertify(t, committee, authority, core, domain, key, b1, genesis)
	b2.Justify = qc1.QC()
	qc2 = acceptAndCertify(t, committee, authority, core, domain, key, b2, qc1)
	b3.Justify = qc2.QC()
	_, err = authority.Accept(core, b3, qc2)
	require.NoError(t, err)
	fork := hotstuff.Block{ID: "fork", Parent: domain.Genesis, View: 1, Justify: genesis.QC()}
	forkQC := acceptAndCertify(t, committee, authority, core, domain, key, fork, genesis)
	conflict := hotstuff.Block{ID: "conflict", Parent: fork.ID, View: 4, Justify: forkQC.QC()}
	_, err = authority.Accept(core, conflict, forkQC)
	require.NoError(t, err)

	restarted := hotstuff.NewSafetyRules(core, restored, store.Save)
	_, err = restarted.Vote("validator-a", conflict)
	require.ErrorIs(t, err, hotstuff.ErrUnsafeProposal)
}

func TestSafetyStoreRoundTripsLockedQCSigners(t *testing.T) {
	db := newTestSafetyDatabase(t)
	domain := hotstuff.VoteDomain{ChainID: 1, ShardID: 2, Epoch: 42, Genesis: "genesis"}
	key := hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey())
	store, err := NewSafetyStore(db, domain, key.Pub.Bytes)
	require.NoError(t, err)
	want := hotstuff.SafetyState{
		LastVotedView: 9,
		LockedQC: hotstuff.QC{
			Block:   "locked-block",
			View:    8,
			Signers: []hotstuff.MemberID{"validator-a", "validator-b"},
		},
	}

	require.NoError(t, store.Save(want))
	got, found, err := store.Load()
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, want, got)
}

func TestSafetyStoreV1EncodingFixture(t *testing.T) {
	const (
		wantKey    = "6861726d6f6e792d686f7473747566662d7361666574792deba7d4b1b5f3e111705fd37118e9b2ebc0213703f18a3278a7970d08b3a601c4"
		wantRecord = "f88301f83c01022a8767656e65736973b00102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f3009866c6f636b65640880d88b76616c696461746f722d618b76616c696461746f722d62a04e27f385f90ec0fad0ee46ddb294a2e6a3f2edf7e2dbddea81ac1c2ae29da093"
	)
	db := newTestSafetyDatabase(t)
	var signer hmybls.SerializedPublicKey
	for index := range signer {
		signer[index] = byte(index + 1)
	}
	store, err := NewSafetyStore(db, hotstuff.VoteDomain{
		ChainID: 1, ShardID: 2, Epoch: 42, Genesis: "genesis",
	}, signer)
	require.NoError(t, err)
	require.Equal(t, wantKey, hex.EncodeToString(store.key))
	require.NoError(t, store.Save(hotstuff.SafetyState{
		LastVotedView: 9,
		LockedQC: hotstuff.QC{
			Block:   "locked",
			View:    8,
			Signers: []hotstuff.MemberID{"validator-a", "validator-b"},
		},
	}))
	record, err := db.Get(store.key)
	require.NoError(t, err)
	require.Equal(t, wantRecord, hex.EncodeToString(record))
}

func TestSafetyStoreReportsMissingRecordAsFirstRun(t *testing.T) {
	db := newTestSafetyDatabase(t)
	store := newTestSafetyStore(t, db, hotstuff.VoteDomain{
		ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis",
	})

	state, found, err := store.Load()
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, hotstuff.SafetyState{}, state)
}

func TestSafetyStoreRejectsCorruptRecord(t *testing.T) {
	db := newTestSafetyDatabase(t)
	store := newTestSafetyStore(t, db, hotstuff.VoteDomain{
		ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis",
	})
	require.NoError(t, db.updateSync(store.key, []byte{0xff}, nil))

	_, found, err := store.Load()
	require.False(t, found)
	require.ErrorIs(t, err, ErrCorruptSafetyStoreRecord)
}

func TestSafetyStoreRejectsTamperedRecord(t *testing.T) {
	db := newTestSafetyDatabase(t)
	store := newTestSafetyStore(t, db, hotstuff.VoteDomain{
		ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis",
	})
	require.NoError(t, store.Save(hotstuff.SafetyState{LastVotedView: 7}))
	encoded, err := db.Get(store.key)
	require.NoError(t, err)
	var record safetyStoreRecord
	require.NoError(t, rlp.DecodeBytes(encoded, &record))
	record.LastVotedView++
	encoded, err = rlp.EncodeToBytes(record)
	require.NoError(t, err)
	require.NoError(t, db.updateSync(store.key, encoded, nil))

	_, found, err := store.Load()
	require.False(t, found)
	require.ErrorIs(t, err, ErrCorruptSafetyStoreRecord)
}

func TestSafetyStoreRejectsUnsupportedVersion(t *testing.T) {
	db := newTestSafetyDatabase(t)
	store := newTestSafetyStore(t, db, hotstuff.VoteDomain{
		ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis",
	})
	require.NoError(t, store.Save(hotstuff.SafetyState{LastVotedView: 7}))
	encoded, err := db.Get(store.key)
	require.NoError(t, err)
	var record safetyStoreRecord
	require.NoError(t, rlp.DecodeBytes(encoded, &record))
	record.Version++
	record.Checksum, err = safetyStoreChecksum(record)
	require.NoError(t, err)
	encoded, err = rlp.EncodeToBytes(record)
	require.NoError(t, err)
	require.NoError(t, db.updateSync(store.key, encoded, nil))

	_, found, err := store.Load()
	require.False(t, found)
	require.ErrorIs(t, err, ErrUnsupportedSafetyStoreVersion)
}

func TestSafetyStoreRejectsMalformedSignerEncoding(t *testing.T) {
	db := newTestSafetyDatabase(t)
	store := newTestSafetyStore(t, db, hotstuff.VoteDomain{
		ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis",
	})
	require.NoError(t, store.Save(hotstuff.SafetyState{LockedQC: hotstuff.QC{
		Block: "locked-block", View: 6, Signers: []hotstuff.MemberID{"validator-a"},
	}}))
	encoded, err := db.Get(store.key)
	require.NoError(t, err)
	var record safetyStoreRecord
	require.NoError(t, rlp.DecodeBytes(encoded, &record))
	record.SignersNil = true
	record.Checksum, err = safetyStoreChecksum(record)
	require.NoError(t, err)
	encoded, err = rlp.EncodeToBytes(record)
	require.NoError(t, err)
	require.NoError(t, db.updateSync(store.key, encoded, nil))

	_, found, err := store.Load()
	require.False(t, found)
	require.ErrorIs(t, err, ErrCorruptSafetyStoreRecord)
}

func TestSafetyStoreRejectsDomainMismatch(t *testing.T) {
	baseDomain := hotstuff.VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	baseKey := hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey()).Pub.Bytes
	otherKey := hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey()).Pub.Bytes
	tests := []struct {
		name   string
		domain hotstuff.VoteDomain
		signer hmybls.SerializedPublicKey
	}{
		{name: "chain", domain: hotstuff.VoteDomain{ChainID: 2, ShardID: 0, Epoch: 42, Genesis: "genesis"}, signer: baseKey},
		{name: "shard", domain: hotstuff.VoteDomain{ChainID: 1, ShardID: 1, Epoch: 42, Genesis: "genesis"}, signer: baseKey},
		{name: "epoch", domain: hotstuff.VoteDomain{ChainID: 1, ShardID: 0, Epoch: 43, Genesis: "genesis"}, signer: baseKey},
		{name: "genesis", domain: hotstuff.VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "other-genesis"}, signer: baseKey},
		{name: "signer", domain: baseDomain, signer: otherKey},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestSafetyDatabase(t)
			first, err := NewSafetyStore(db, baseDomain, baseKey)
			require.NoError(t, err)
			second, err := NewSafetyStore(db, test.domain, test.signer)
			require.NoError(t, err)
			require.NoError(t, first.Save(hotstuff.SafetyState{LastVotedView: 7}))
			record, err := db.Get(first.key)
			require.NoError(t, err)
			require.NoError(t, db.updateSync(second.key, record, nil))

			_, found, err := second.Load()
			require.False(t, found)
			require.ErrorIs(t, err, ErrSafetyStoreDomainMismatch)
		})
	}
}

func TestSafetyStoreWriteFailureDoesNotAdvanceSafetyState(t *testing.T) {
	injected := errors.New("injected safety store write failure")
	db := &failingPutDatabase{
		safetyStoreDatabase: newTestSafetyDatabase(t),
		err:                 injected,
	}
	domain := hotstuff.VoteDomain{ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis"}
	key := hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey())
	store, err := newSafetyStore(db, domain, key.Pub.Bytes)
	require.NoError(t, err)
	core, genesis, proposal := safetyRuntime(t, domain, *key.Pub)
	initial := hotstuff.SafetyState{LockedQC: genesis.QC()}
	rules := hotstuff.NewSafetyRules(core, initial, store.Save)

	_, err = rules.Vote("validator-a", proposal)
	require.ErrorIs(t, err, injected)
	require.Equal(t, initial, rules.State())
}

func TestSafetyStoreReadFailureIsNotFirstRun(t *testing.T) {
	injected := errors.New("injected safety store read failure")
	db := &failingHasDatabase{
		safetyStoreDatabase: newTestSafetyDatabase(t),
		err:                 injected,
	}
	store := newTestSafetyStore(t, db, hotstuff.VoteDomain{
		ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis",
	})

	_, found, err := store.Load()
	require.False(t, found)
	require.ErrorIs(t, err, injected)
}

func TestSafetyStoreGetFailureIsNotFirstRun(t *testing.T) {
	injected := errors.New("injected safety store get failure")
	db := &failingGetDatabase{
		safetyStoreDatabase: newTestSafetyDatabase(t),
		err:                 injected,
	}
	store := newTestSafetyStore(t, db, hotstuff.VoteDomain{
		ChainID: 1, ShardID: 0, Epoch: 42, Genesis: "genesis",
	})

	_, found, err := store.Load()
	require.False(t, found)
	require.ErrorIs(t, err, injected)
}

func newTestSafetyStore(
	t *testing.T,
	db safetyStoreDatabase,
	domain hotstuff.VoteDomain,
) *SafetyStore {
	t.Helper()
	key := hmybls.WrapperFromPrivateKey(hmybls.RandPrivateKey())
	store, err := newSafetyStore(db, domain, key.Pub.Bytes)
	require.NoError(t, err)
	return store
}

type failingPutDatabase struct {
	safetyStoreDatabase
	err error
}

func (db *failingPutDatabase) updateSync(
	[]byte,
	[]byte,
	func(current []byte, found bool) error,
) error {
	return db.err
}

type failingHasDatabase struct {
	safetyStoreDatabase
	err error
}

func (db *failingHasDatabase) Has([]byte) (bool, error) {
	return false, db.err
}

type failingGetDatabase struct {
	safetyStoreDatabase
	err error
}

func (db *failingGetDatabase) Has([]byte) (bool, error) {
	return true, nil
}

func (db *failingGetDatabase) Get([]byte) ([]byte, error) {
	return nil, db.err
}

func openTestSafetyDatabase(
	t *testing.T,
	path string,
) *LevelDBSafetyStoreDatabase {
	t.Helper()
	db, err := OpenLevelDBSafetyStoreDatabase(path)
	require.NoError(t, err)
	return db
}

func newTestSafetyDatabase(t *testing.T) *LevelDBSafetyStoreDatabase {
	t.Helper()
	db := openTestSafetyDatabase(t, filepath.Join(t.TempDir(), "safety"))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

type recordingLevelDB struct {
	writeOptions *opt.WriteOptions
}

func (db *recordingLevelDB) Has([]byte, *opt.ReadOptions) (bool, error) {
	return false, nil
}

func (db *recordingLevelDB) Get([]byte, *opt.ReadOptions) ([]byte, error) {
	return nil, nil
}

func (db *recordingLevelDB) Put(_ []byte, _ []byte, options *opt.WriteOptions) error {
	db.writeOptions = options
	return nil
}

type coordinatedLevelDB struct {
	mu        sync.Mutex
	values    map[string][]byte
	hasCalls  int
	secondHas chan struct{}
}

func newCoordinatedLevelDB() *coordinatedLevelDB {
	return &coordinatedLevelDB{
		values:    map[string][]byte{},
		secondHas: make(chan struct{}),
	}
}

func (db *coordinatedLevelDB) Has(key []byte, _ *opt.ReadOptions) (bool, error) {
	db.mu.Lock()
	_, found := db.values[string(key)]
	if found {
		db.mu.Unlock()
		return true, nil
	}
	db.hasCalls++
	if db.hasCalls == 2 {
		close(db.secondHas)
	}
	secondHas := db.secondHas
	db.mu.Unlock()

	select {
	case <-secondHas:
	case <-time.After(50 * time.Millisecond):
	}
	return false, nil
}

func (db *coordinatedLevelDB) Get(key []byte, _ *opt.ReadOptions) ([]byte, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	return append([]byte(nil), db.values[string(key)]...), nil
}

func (db *coordinatedLevelDB) Put(key, value []byte, _ *opt.WriteOptions) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.values[string(key)] = append([]byte(nil), value...)
	return nil
}

func safetyRuntime(
	t *testing.T,
	domain hotstuff.VoteDomain,
	publicKey hmybls.PublicKeyWrapper,
) (*hotstuff.Core, hotstuff.VerifiedQC, hotstuff.Block) {
	t.Helper()
	_, authority, core, genesis := safetyAuthority(t, domain, publicKey)
	proposal := hotstuff.Block{
		ID:      "block-1",
		Parent:  domain.Genesis,
		View:    1,
		Justify: genesis.QC(),
	}
	_, err := authority.Accept(core, proposal, genesis)
	require.NoError(t, err)
	return core, genesis, proposal
}

func safetyAuthority(
	t *testing.T,
	domain hotstuff.VoteDomain,
	publicKey hmybls.PublicKeyWrapper,
) (*hotstuff.BLSCommittee, *hotstuff.QCAuthority, *hotstuff.Core, hotstuff.VerifiedQC) {
	t.Helper()
	committee, err := hotstuff.NewBLSCommitteeFromValidatedKeys([]hotstuff.BLSMember{{
		Member:    hotstuff.Member{ID: "validator-a", Power: 1},
		PublicKey: publicKey,
	}})
	require.NoError(t, err)
	authority := hotstuff.NewQCAuthority(committee, domain)
	core, genesis, err := authority.NewCore(hotstuff.Block{ID: domain.Genesis, View: 0})
	require.NoError(t, err)
	return committee, authority, core, genesis
}

func acceptAndCertify(
	t *testing.T,
	committee *hotstuff.BLSCommittee,
	authority *hotstuff.QCAuthority,
	core *hotstuff.Core,
	domain hotstuff.VoteDomain,
	key hmybls.PrivateKeyWrapper,
	block hotstuff.Block,
	justify hotstuff.VerifiedQC,
) hotstuff.VerifiedQC {
	t.Helper()
	_, err := authority.Accept(core, block, justify)
	require.NoError(t, err)
	vote, err := hotstuff.SignVote(domain, hotstuff.Vote{
		Voter: "validator-a", Block: block.ID, View: block.View,
	}, key.Pri)
	require.NoError(t, err)
	votes := hotstuff.NewBLSVoteSet(committee, block.ID, block.View, domain)
	require.NoError(t, votes.Add(vote))
	certificate, formed, err := votes.QC()
	require.NoError(t, err)
	require.True(t, formed)
	verified, err := authority.Verify(certificate)
	require.NoError(t, err)
	return verified
}
