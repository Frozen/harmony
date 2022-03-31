package main

import (
	"fmt"

	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/leveldb"
	"github.com/harmony-one/harmony/internal/shardchain"
)

func p(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	db, err := leveldb.New("/data/harmony_db_0", 256, 1024, "")
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

	var batch ethdb.Batch

	i := 0
	for iter.Next() {
		// Remember that the contents of the returned slice should not be modified, and
		// only valid until the next call to Next.
		if batch == nil {
			batch = chainDB.NewBatch()
		}
		key := iter.Key()
		err := batch.Put(key, iter.Value())
		p(err)
		fmt.Println(string(key))
		if i > 0 && i%10000 == 0 {
			err := batch.Write()
			p(err)
			batch = nil
		}
		i++
	}
	if batch != nil {
		err := batch.Write()
		p(err)
	}
	iter.Release()

	iter.Key()
}
