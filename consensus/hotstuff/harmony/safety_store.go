package harmony

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/harmony-one/harmony/consensus/hotstuff"
	hmybls "github.com/harmony-one/harmony/crypto/bls"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

const (
	safetyStoreVersion   uint64 = 1
	safetyStoreKeyPrefix        = "harmony-hotstuff-safety-"
)

var (
	// ErrNilSafetyStoreDatabase rejects a store without a persistence backend.
	ErrNilSafetyStoreDatabase = errors.New("hotstuff safety store database is nil")
	// ErrInvalidSafetyStoreScope rejects an incomplete authority or signer scope.
	ErrInvalidSafetyStoreScope = errors.New("hotstuff safety store scope is invalid")
	// ErrCorruptSafetyStoreRecord prevents voting from damaged persisted state.
	ErrCorruptSafetyStoreRecord = errors.New("hotstuff safety store record is corrupt")
	// ErrSafetyStoreDomainMismatch prevents state reuse across authorities.
	ErrSafetyStoreDomainMismatch = errors.New("hotstuff safety store record belongs to another domain")
	// ErrUnsupportedSafetyStoreVersion prevents an unknown record from looking like first run.
	ErrUnsupportedSafetyStoreVersion = errors.New("hotstuff safety store record version is unsupported")
	// ErrInvalidSafetyStorePath rejects an empty dedicated LevelDB path.
	ErrInvalidSafetyStorePath = errors.New("hotstuff safety store path is invalid")
	// ErrSafetyStoreStateRegression rejects duplicate votes and lock rollback across writers.
	ErrSafetyStoreStateRegression = errors.New("hotstuff safety store transition regresses durable state")
)

type levelDBDatabase interface {
	Has(key []byte, options *opt.ReadOptions) (bool, error)
	Get(key []byte, options *opt.ReadOptions) ([]byte, error)
	Put(key, value []byte, options *opt.WriteOptions) error
}

// LevelDBSafetyStoreDatabase makes each acknowledged safety-state write reach
// stable storage before a vote can be returned.
type LevelDBSafetyStoreDatabase struct {
	db    levelDBDatabase
	state *levelDBSafetyStoreDatabaseState
}

type levelDBSafetyStoreDatabaseState struct {
	mu     sync.Mutex
	close  func() error
	closed bool
}

type safetyStoreDatabase interface {
	Has(key []byte) (bool, error)
	Get(key []byte) ([]byte, error)
	updateSync(key, value []byte, validate func(current []byte, found bool) error) error
}

// OpenLevelDBSafetyStoreDatabase opens and owns a dedicated LevelDB safety
// database. Global NoSync mode is deliberately disabled so synchronous writes cannot be
// downgraded by caller-supplied database options.
func OpenLevelDBSafetyStoreDatabase(path string) (*LevelDBSafetyStoreDatabase, error) {
	if path == "" {
		return nil, ErrInvalidSafetyStorePath
	}
	rawDB, err := leveldb.OpenFile(path, &opt.Options{NoSync: false})
	if err != nil {
		return nil, fmt.Errorf("open hotstuff safety store database: %w", err)
	}
	return &LevelDBSafetyStoreDatabase{
		db: rawDB,
		state: &levelDBSafetyStoreDatabaseState{
			close: rawDB.Close,
		},
	}, nil
}

func newLevelDBSafetyStoreDatabase(db levelDBDatabase) *LevelDBSafetyStoreDatabase {
	return &LevelDBSafetyStoreDatabase{
		db:    db,
		state: &levelDBSafetyStoreDatabaseState{},
	}
}

// Close releases the dedicated LevelDB safety database.
func (db *LevelDBSafetyStoreDatabase) Close() error {
	if db == nil || db.state == nil {
		return ErrNilSafetyStoreDatabase
	}
	db.state.mu.Lock()
	defer db.state.mu.Unlock()
	if db.state.closed {
		return nil
	}
	db.state.closed = true
	if db.state.close == nil {
		return nil
	}
	return db.state.close()
}

// Has reports whether a safety record exists.
func (db *LevelDBSafetyStoreDatabase) Has(key []byte) (bool, error) {
	db.state.mu.Lock()
	defer db.state.mu.Unlock()
	if db.state.closed {
		return false, leveldb.ErrClosed
	}
	return db.db.Has(key, nil)
}

// Get reads a safety record.
func (db *LevelDBSafetyStoreDatabase) Get(key []byte) ([]byte, error) {
	db.state.mu.Lock()
	defer db.state.mu.Unlock()
	if db.state.closed {
		return nil, leveldb.ErrClosed
	}
	return db.db.Get(key, nil)
}

func (db *LevelDBSafetyStoreDatabase) updateSync(
	key []byte,
	value []byte,
	validate func(current []byte, found bool) error,
) error {
	db.state.mu.Lock()
	defer db.state.mu.Unlock()
	if db.state.closed {
		return leveldb.ErrClosed
	}

	found, err := db.db.Has(key, nil)
	if err != nil {
		return err
	}
	var current []byte
	if found {
		current, err = db.db.Get(key, nil)
		if err != nil {
			return err
		}
	}
	if validate != nil {
		if err := validate(current, found); err != nil {
			return err
		}
	}
	return db.db.Put(key, value, &opt.WriteOptions{Sync: true})
}

type safetyStoreScope struct {
	ChainID uint32
	ShardID uint32
	Epoch   uint64
	Genesis string
	Signer  []byte
}

type safetyStoreRecord struct {
	Version       uint64
	Scope         safetyStoreScope
	LastVotedView uint64
	LockedBlock   string
	LockedView    uint64
	SignersNil    bool
	LockedSigners []string
	Checksum      []byte
}

// SafetyStore persists the minimum HotStuff state required to prevent a
// validator from voting unsafely after a machine restart.
type SafetyStore struct {
	db    safetyStoreDatabase
	key   []byte
	scope safetyStoreScope
}

// NewSafetyStore binds a safety store to one HotStuff authority domain and one
// local BLS voting key. The consensus lifecycle must maintain exactly one
// active SafetyStore and SafetyRules writer for each domain and signer.
func NewSafetyStore(
	db *LevelDBSafetyStoreDatabase,
	domain hotstuff.VoteDomain,
	signer hmybls.SerializedPublicKey,
) (*SafetyStore, error) {
	if db == nil || db.db == nil || db.state == nil {
		return nil, ErrNilSafetyStoreDatabase
	}
	return newSafetyStore(db, domain, signer)
}

func newSafetyStore(
	db safetyStoreDatabase,
	domain hotstuff.VoteDomain,
	signer hmybls.SerializedPublicKey,
) (*SafetyStore, error) {
	if domain.Genesis == "" || signer.IsEmpty() {
		return nil, ErrInvalidSafetyStoreScope
	}
	scope := safetyStoreScope{
		ChainID: domain.ChainID,
		ShardID: domain.ShardID,
		Epoch:   domain.Epoch,
		Genesis: string(domain.Genesis),
		Signer:  append([]byte(nil), signer.Bytes()...),
	}
	encoded, err := rlp.EncodeToBytes(scope)
	if err != nil {
		return nil, fmt.Errorf("encode hotstuff safety store scope: %w", err)
	}
	digest := sha256.Sum256(encoded)
	key := append([]byte(safetyStoreKeyPrefix), digest[:]...)
	return &SafetyStore{db: db, key: key, scope: scope}, nil
}

// Save atomically replaces the complete safety state for this store's domain.
func (s *SafetyStore) Save(state hotstuff.SafetyState) error {
	record := safetyStoreRecord{
		Version:       safetyStoreVersion,
		Scope:         s.scope,
		LastVotedView: uint64(state.LastVotedView),
		LockedBlock:   string(state.LockedQC.Block),
		LockedView:    uint64(state.LockedQC.View),
		SignersNil:    state.LockedQC.Signers == nil,
		LockedSigners: make([]string, len(state.LockedQC.Signers)),
	}
	for index, signer := range state.LockedQC.Signers {
		record.LockedSigners[index] = string(signer)
	}
	checksum, err := safetyStoreChecksum(record)
	if err != nil {
		return fmt.Errorf("checksum hotstuff safety state: %w", err)
	}
	record.Checksum = checksum
	encoded, err := rlp.EncodeToBytes(record)
	if err != nil {
		return fmt.Errorf("encode hotstuff safety state: %w", err)
	}
	validate := func(current []byte, found bool) error {
		if !found {
			return nil
		}
		previous, err := s.decode(current)
		if err != nil {
			return err
		}
		if state.LastVotedView <= previous.LastVotedView ||
			state.LockedQC.View < previous.LockedQC.View ||
			(state.LockedQC.View == previous.LockedQC.View &&
				state.LockedQC.Block != previous.LockedQC.Block) {
			return ErrSafetyStoreStateRegression
		}
		return nil
	}
	if err := s.db.updateSync(s.key, encoded, validate); err != nil {
		return fmt.Errorf("persist hotstuff safety state: %w", err)
	}
	return nil
}

// Load returns the durable safety state. A false found result means this voting
// key has no state in this authority domain yet.
func (s *SafetyStore) Load() (hotstuff.SafetyState, bool, error) {
	exists, err := s.db.Has(s.key)
	if err != nil {
		return hotstuff.SafetyState{}, false, fmt.Errorf("check hotstuff safety state: %w", err)
	}
	if !exists {
		return hotstuff.SafetyState{}, false, nil
	}
	encoded, err := s.db.Get(s.key)
	if err != nil {
		return hotstuff.SafetyState{}, false, fmt.Errorf("read hotstuff safety state: %w", err)
	}
	state, err := s.decode(encoded)
	if err != nil {
		return hotstuff.SafetyState{}, false, err
	}
	return state, true, nil
}

func (s *SafetyStore) decode(encoded []byte) (hotstuff.SafetyState, error) {
	var record safetyStoreRecord
	if err := rlp.DecodeBytes(encoded, &record); err != nil {
		return hotstuff.SafetyState{}, fmt.Errorf("%w: %v", ErrCorruptSafetyStoreRecord, err)
	}
	if record.Version != safetyStoreVersion {
		return hotstuff.SafetyState{}, fmt.Errorf("%w: %d", ErrUnsupportedSafetyStoreVersion, record.Version)
	}
	if !sameSafetyStoreScope(record.Scope, s.scope) {
		return hotstuff.SafetyState{}, ErrSafetyStoreDomainMismatch
	}
	checksum, err := safetyStoreChecksum(record)
	if err != nil || !bytes.Equal(record.Checksum, checksum) {
		return hotstuff.SafetyState{}, ErrCorruptSafetyStoreRecord
	}
	if record.SignersNil && len(record.LockedSigners) != 0 {
		return hotstuff.SafetyState{}, ErrCorruptSafetyStoreRecord
	}
	state := hotstuff.SafetyState{
		LastVotedView: hotstuff.View(record.LastVotedView),
		LockedQC: hotstuff.QC{
			Block: hotstuff.BlockID(record.LockedBlock),
			View:  hotstuff.View(record.LockedView),
		},
	}
	if !record.SignersNil {
		state.LockedQC.Signers = make([]hotstuff.MemberID, len(record.LockedSigners))
	}
	for index, signer := range record.LockedSigners {
		state.LockedQC.Signers[index] = hotstuff.MemberID(signer)
	}
	return state, nil
}

func safetyStoreChecksum(record safetyStoreRecord) ([]byte, error) {
	record.Checksum = nil
	encoded, err := rlp.EncodeToBytes(record)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	return digest[:], nil
}

func sameSafetyStoreScope(left, right safetyStoreScope) bool {
	if left.ChainID != right.ChainID ||
		left.ShardID != right.ShardID ||
		left.Epoch != right.Epoch ||
		left.Genesis != right.Genesis ||
		len(left.Signer) != len(right.Signer) {
		return false
	}
	for index := range left.Signer {
		if left.Signer[index] != right.Signer[index] {
			return false
		}
	}
	return true
}
