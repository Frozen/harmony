package txfees

import (
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/common"
	rawdb2 "github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/harmony-one/harmony/core/rawdb"
	"github.com/harmony-one/harmony/core/types"
	"github.com/harmony-one/harmony/internal/utils"
	"gopkg.in/urfave/cli.v1"
)

func main() {
	a := cli.NewApp()
	a.Version = "1.0.0"
	a.Name = "vf app"
	a.Usage = "cli for vf app"
	a.Commands = []cli.Command{
		{
			Name:   "run",
			Usage:  "Generate and write result",
			Action: cmdRun,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "path",
					Value: "",
					Usage: "Specify path to source file",
					//Required: true,
				},
				cli.Int64Flag{
					Name:  "block",
					Value: 0,
					Usage: "Specify starting block number",
					//Required: true,
				},
				//cli.StringFlag{
				//	Name:     argNameTargetPackage,
				//	Value:    "",
				//	Usage:    "Specify target package name",
				//	Required: true,
				//},
			},
		},
	}

	if err := a.Run(os.Args); err != nil {
		utils.Logger().Err(err).Msg("failed to run command")
		fmt.Printf("%v\n", err)
		//panic("cannot run command: " + err.Error())
		os.Exit(1)
	}
}

func getDB(path string) ethdb.Database {
	db, err := rawdb2.NewLevelDBDatabase(path, 256, 1024, "")
	if err != nil {
		panic(err)
	}
	return db
}

func GetReceiptsByHash(db ethdb.Database, hash common.Hash) types.Receipts {
	number := rawdb.ReadHeaderNumber(db, hash)
	if number == nil {
		return nil
	}

	receipts := rawdb.ReadReceipts(db, hash, *number)
	return receipts
}

func GetBlockByNumber(db ethdb.Database, number uint64) *types.Block {
	hash := rawdb.ReadCanonicalHash(db, number)
	if hash == (common.Hash{}) {
		return nil
	}
	block := rawdb.ReadBlock(db, hash, number)
	return block
}

func cmdRun(c *cli.Context) error {
	path := c.String("path")
	if path == "" {
		return fmt.Errorf("path is required")
	}
	db := getDB(path)
	defer db.Close()

	f, err := os.Create("txfees.txt")
	if err != nil {
		panic(err)
	}

	start := c.Int64("block")
	if start == 0 {
		return fmt.Errorf("block is required")
	}
	for block := GetBlockByNumber(db, uint64(start)); block != nil; start++ {
		for _, tx := range block.Transactions() {
			receipts := GetReceiptsByHash(db, tx.Hash())
			if len(receipts) == 0 {
				fmt.Println("no receipt for tx:", tx.Hash().Hex())
			}
			for _, receipt := range receipts {
				fmt.Fprintf(f, "%d %s %d\n", block.NumberU64(), tx.Hash().Hex(), receipt.CumulativeGasUsed)
			}

		}
		for _, tx := range block.StakingTransactions() {
			receipts := GetReceiptsByHash(db, tx.Hash())
			for _, receipt := range receipts {
				fmt.Fprintf(f, "%d %s %d\n", block.NumberU64(), tx.Hash().Hex(), receipt.CumulativeGasUsed)
			}
		}
	}
	return nil
}
