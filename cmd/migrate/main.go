package main

import (
	"fmt"

	"github.com/ethereum/go-ethereum/ethdb/leveldb"
	"github.com/harmony-one/harmony/internal/shardchain"
)

func p(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	db, err := leveldb.New("/data", 256, 1024, "")
	p(err)
	iter := db.NewIterator()

	shardDb := shardchain.LDBShardFactory{
		RootDir:    "/data",
		DiskCount:  8,
		ShardCount: 4,
		CacheTime:  10,
		CacheSize:  512,
	}

	chainDB, err := shardDb.NewChainDB(0)
	p(err)

	for iter.Next() {
		// Remember that the contents of the returned slice should not be modified, and
		// only valid until the next call to Next.
		key := iter.Key()
		err := chainDB.Put(key, iter.Value())
		fmt.Println(string(key))
		p(err)
	}
	iter.Release()

	iter.Key()
}
