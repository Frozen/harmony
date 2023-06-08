package kv

import (
	"context"
	"sync"

	"github.com/pkg/errors"
)

const SyncStageProgress = "SyncStage"

var ErrorNotFound = errors.New("not found")

type Getter interface {
	//Has

	// GetOne references a readonly section of memory that must not be accessed after txn has terminated
	GetOne(bucket string, key []byte) (val []byte, err error)

	//// ForEach iterates over entries with keys greater or equal to fromPrefix.
	//// walker is called for each eligible entry.
	//// If walker returns an error:
	////   - implementations of local db - stop
	////   - implementations of remote db - do not handle this error and may finish (send all entries to client) before error happen.
	//ForEach(bucket string, fromPrefix []byte, walker func(k, v []byte) error) error
	//ForPrefix(bucket string, prefix []byte, walker func(k, v []byte) error) error
	//ForAmount(bucket string, prefix []byte, amount uint32, walker func(k, v []byte) error) error
}

// Putter wraps the database write operations.
type Putter interface {
	// Put inserts or updates a single entry.
	Put(table string, k, v []byte) error
}

type RwDB interface {
	// View is read-only transaction.
	View(ctx context.Context, f func(tx Tx) error) error

	BeginRw(ctx context.Context) (RwTx, error)

	Update(ctx context.Context, f func(tx RwTx) error) error
}

type DB struct {
	mu sync.RWMutex
	v  map[string]map[string][]byte
}

// View is read only transaction.
func (a *DB) View(ctx context.Context, f func(tx Tx) error) error {
	return f(newTxImpl(a))
}

func (a *DB) BeginRw(ctx context.Context) (RwTx, error) {
	return newTxImpl(a), nil
}

func (a *DB) Update(ctx context.Context, f func(tx RwTx) error) error {
	return f(newTxImpl(a))
}

func newDB() *DB {
	return &DB{
		v: make(map[string]map[string][]byte),
	}
}

func NewDB() *DB {
	return newDB()
}

// Tx is a transaction.
type Tx interface {
	Getter
}

type RwTx interface {
	Tx
	Put(table string, k, v []byte) error
	Commit() error
	Rollback()
	ClearBucket(table string) error
}

type TxImpl struct {
	db           *DB
	v            map[string]map[string][]byte
	clearBuckets []string
}

func newTxImpl(db *DB) *TxImpl {
	return &TxImpl{
		db: db,
		v:  make(map[string]map[string][]byte),
	}
}

func (a *TxImpl) Put(table string, k, v []byte) error {
	if _, ok := a.v[table]; !ok {
		a.v[table] = make(map[string][]byte)
	}
	a.v[table][string(k)] = v
	return nil
}

func (a *TxImpl) Commit() error {
	db := a.db
	db.mu.Lock()
	defer db.mu.Unlock()
	for table, kv := range a.v {
		for k, v := range kv {
			if inner, ok := db.v[table]; ok {
				inner[k] = v
			} else {
				db.v[table] = make(map[string][]byte)
				db.v[table][k] = v
			}
		}
	}
	for _, table := range a.clearBuckets {
		db.v[table] = make(map[string][]byte)
	}
	a.v = nil // no use after commit
	a.db = nil
	a.clearBuckets = nil
	return nil
}

func (a *TxImpl) Rollback() {
	a.v = nil // no use after rollback
}

func (a *TxImpl) ClearBucket(table string) error {
	a.clearBuckets = append(a.clearBuckets, table)
	return nil
}

func (a *TxImpl) GetOne(bucket string, key []byte) (val []byte, err error) {
	if kv, ok := a.v[bucket]; ok {
		if v, ok := kv[string(key)]; ok {
			return v, nil
		}
	}
	a.db.mu.RLock()
	defer a.db.mu.RUnlock()
	if kv, ok := a.db.v[bucket]; ok {
		if v, ok := kv[string(key)]; ok {
			return v, nil
		}
	}
	return nil, ErrorNotFound
}
