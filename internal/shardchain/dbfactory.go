package shardchain

import (
	"fmt"
	"path"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/ethdb/leveldb"
	"github.com/harmony-one/harmony/internal/shardchain/leveldb_shard"
	"github.com/harmony-one/harmony/internal/shardchain/local_cache"

	"github.com/ethereum/go-ethereum/core/rawdb"
	corerawdb "github.com/harmony-one/harmony/core/rawdb"

	"github.com/ethereum/go-ethereum/ethdb"
)

const (
	LDBDirPrefix      = "harmony_db"
	LDBShardDirPrefix = "harmony_sharddb"
)

// DBFactory is a blockchain database factory.
type DBFactory interface {
	// NewChainDB returns a new database for the blockchain for
	// given shard.
	NewChainDB(shardID uint32) (ethdb.Database, error)
}

// LDBFactory is a LDB-backed blockchain database factory.
type LDBFactory struct {
	RootDir string // directory in which to put shard databases in.
}

// NewChainDB returns a new LDB for the blockchain for given shard.
func (f *LDBFactory) NewChainDB(shardID uint32) (ethdb.Database, error) {
	dir := path.Join(f.RootDir, fmt.Sprintf("%s_%d", LDBDirPrefix, shardID))
	return NewLevelDBDatabase(dir, 256, 1024, "")
}

// NewLevelDBDatabase creates a persistent key-value database without a freezer
// moving immutable chain segments into cold storage.
func NewLevelDBDatabase(file string, cache int, handles int, namespace string) (ethdb.Database, error) {
	db, err := leveldb.New(file, cache, handles, namespace)
	if err != nil {
		return nil, err
	}
	return rawdb.NewDatabase(corerawdb.NewPrometheusWrapper(db, "lvl_db")), nil
}

// MemDBFactory is a memory-backed blockchain database factory.
type MemDBFactory struct{}

// NewChainDB returns a new memDB for the blockchain for given shard.
func (f *MemDBFactory) NewChainDB(shardID uint32) (ethdb.Database, error) {
	return rawdb.NewMemoryDatabase(), nil
}

// LDBShardFactory is a merged Multi-LDB-backed blockchain database factory.
type LDBShardFactory struct {
	RootDir    string // directory in which to put shard databases in.
	DiskCount  int
	ShardCount int
	CacheTime  int
	CacheSize  int
}

// NewChainDB returns a new memDB for the blockchain for given shard.
func (f *LDBShardFactory) NewChainDB(shardID uint32) (ethdb.Database, error) {
	dir := filepath.Join(f.RootDir, fmt.Sprintf("%s_%d", LDBShardDirPrefix, shardID))
	shard, err := leveldb_shard.NewLeveldbShard(dir, f.DiskCount, f.ShardCount)
	if err != nil {
		return nil, err
	}

	return rawdb.NewDatabase(corerawdb.NewPrometheusWrapper(local_cache.NewLocalCacheDatabase(shard, local_cache.CacheConfig{
		CacheTime: time.Duration(f.CacheTime) * time.Minute,
		CacheSize: f.CacheSize,
	}), "shard_db")), nil
}
