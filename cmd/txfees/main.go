package main

import (
	"bufio"
	"context"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethereum/go-ethereum/common"
	rawdb2 "github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/harmony-one/harmony/core/rawdb"
	"github.com/harmony-one/harmony/core/types"
	"github.com/harmony-one/harmony/internal/utils"
	"github.com/shopspring/decimal"
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
			Usage:  "Retrieve all fees",
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
				cli.StringFlag{
					Name:  "output",
					Value: "",
					Usage: "Specify output file",
					//Required: true,
				},
				cli.Int64Flag{
					Name:  "count",
					Value: 0,
					Usage: "Specify number of blocks to process",
				},
				//cli.StringFlag{
				//	Name:     argNameTargetPackage,
				//	Value:    "",
				//	Usage:    "Specify target package name",
				//	Required: true,
				//},
			},
		},
		{
			Name:   "calc",
			Usage:  "calc fees",
			Action: cmdCalc,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "path",
					Value: "",
					Usage: "Specify path to source file",
					//Required: true,
				},
				cli.Int64Flag{
					Name:  "start",
					Value: 0,
					Usage: "start from block",
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

func GetBlockHash(db ethdb.Database, number uint64) *common.Hash {
	hash := rawdb.ReadCanonicalHash(db, number)
	if hash == (common.Hash{}) {
		return nil
	}
	return &hash
}

func cmdRun(c *cli.Context) error {
	path := c.String("path")
	if path == "" {
		return fmt.Errorf("path is required")
	}
	count := c.Int64("count")
	db := getDB(path)
	defer db.Close()

	f, err := os.Create(c.String("output"))
	if err != nil {
		panic(err)
	}
	defer f.Close()

	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill, syscall.SIGTERM)

	start := c.Int64("block")
	if start == 0 {
		return fmt.Errorf("block is required")
	}

	buf := bufio.NewWriter(f)
	defer buf.Flush()

	for {

		if count <= 0 {
			fmt.Println("stop on count")
			break
		}

		select {
		case <-ctx.Done():
			fmt.Println("exit on ctx.Done()")
			break
		default:
		}

		block := GetBlockByNumber(db, uint64(start))
		if block == nil {
			fmt.Printf("block %d not found\n", start)
			break
		}
		var prices []*big.Int
		for _, tx := range block.Transactions() {
			prices = append(prices, tx.GasPrice())
		}
		for _, tx := range block.StakingTransactions() {
			prices = append(prices, tx.GasPrice())
		}
		//fmt.Println("block:", block)
		fmt.Printf("Proceeding block: %d\n", start)
		//for _, tx := range block.Transactions() {
		receipts := GetReceiptsByHash(db, block.Hash())
		//if len(receipts) == 0 {
		//	fmt.Println("no receipt for block: ", start)
		//}
		for i, receipt := range receipts {
			//fmt.Printf("r: %+v\n", receipt.CumulativeGasUsed)
			_, err := buf.WriteString(fmt.Sprintf("%d %d %s\n", start, i, prices[i].Mul(prices[i], big.NewInt(int64(receipt.GasUsed))).String()))
			if err != nil {
				panic(err)
			}
			//fmt.Fprintf(f, "%d %d %d\n", start, i, receipt.CumulativeGasUsed)
		}
		//}
		//for _, tx := range block.StakingTransactions() {
		//	receipts := GetReceiptsByHash(db, tx.Hash())
		//	for _, receipt := range receipts {
		//		fmt.Fprintf(f, "%d %s %d\n", block.NumberU64(), tx.Hash().Hex(), receipt.CumulativeGasUsed)
		//	}
		//}
		count--
		start++
	}
	return nil
}

func cmdCalc(c *cli.Context) error {
	fmt.Println("start ", c.Int64("start"))
	path := c.String("path")
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	r := bufio.NewReader(f)

	var (
		start    uint64
		i        int
		gasS     string
		totalGas decimal.Decimal
	)

	for {
		l, _, err := r.ReadLine()
		if err != nil {
			fmt.Println(err)
			break
		}

		_, err = fmt.Sscanf(string(l), "%d %d %s", &start, &i, &gasS)
		//_, err = fmt.Fscan(r, "%d %d %d", &start, &i, &gas)
		if err != nil {
			fmt.Println("error scan:", err)
			break
		}

		if start <= uint64(c.Int64("start")) {
			fmt.Println("skip", start)
			continue
		}

		if start%100000 == 0 {
			fmt.Println("at: ", start)
		}

		price, err := decimal.NewFromString(gasS)
		if err != nil {
			fmt.Println("error parse:", err)
			break
		}

		totalGas = totalGas.Add(price)

		//if totalGas > totalGas+gas {
		//	panic(fmt.Sprintf("gas overflow on %d %d %d", start, totalGas, gas))
		//}

		//totalGas += gas

	}

	fmt.Println("Total gas:", totalGas.String())
	fmt.Println("Total gas:", totalGas.Div(decimal.NewFromInt(10_000_000_000)).String())
	return nil
}
