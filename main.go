package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ngaut/log"
)

var defaultPushMetricsInterval = 15 * time.Second

var (
	dbName      = flag.String("db", "test", "database name")
	pw          = flag.String("pw", "", "database password")
	user        = flag.String("user", "root", "database user")
	accounts    = flag.Int("accounts", 1000000, "the number of accounts")
	interval    = flag.Duration("interval", 2*time.Second, "the interval")
	tables      = flag.Int("tables", 1, "the number of the tables")
	concurrency = flag.Int("concurrency", 200, "concurrency worker count")
	retryLimit  = flag.Int("retry-limit", 200, "retry count")
	longTxn     = flag.Bool("long-txn", true, "enable long-term transactions")
	pessimistic = flag.Bool("pessimistic", false, "use pessimistic transaction")
	dbAddr      = flag.String("addr", "", "the address of db")
)

var (
	defaultVerifyTimeout = 6 * time.Hour
	remark               = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXVZabcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXVZlkjsanksqiszndqpijdslnnq"
)
var (
	TiDBDatabase = true
)

func main() {
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())

	dbDSN := fmt.Sprintf("%s:%s@tcp(%s)/%s", *user, *pw, *dbAddr, *dbName)
	log.Info(dbDSN)
	db, err := OpenDB(dbDSN, 1)
	if err != nil {
		log.Fatalf("[bank] create dlog error %v", err)
	}
	_, err = db.Exec("select tidb_version();")
	if err != nil {
		TiDBDatabase = false
		log.Info("[bank] select tidb_version(): %v", err)
	}

	if TiDBDatabase {
		if *pessimistic {
			_, err = db.Exec("set @@global.tidb_txn_mode = 'pessimistic';")
			if err != nil {
				log.Fatalf("[bank] set pessimistic failed: %v", err)
			}
		}

		var txnMode string
		if err = db.QueryRow("select @@tidb_txn_mode").Scan(&txnMode); err == nil {
			log.Infof("[bank] Current txmode: %v", txnMode)
		}
	}

	err = db.Close()
	if err != nil {
		log.Fatalf("[bank] fail to close set txmode conn", err)
	}

	time.Sleep(5 * time.Second)

	db, err = OpenDB(dbDSN, *concurrency)
	if err != nil {
		log.Fatalf("[bank] create dlog error %v", err)
	}

	sc := make(chan os.Signal, 1)
	signal.Notify(sc,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT)
	go func() {
		sig := <-sc
		log.Infof("[bank] Got signal [%s] to exist.", sig)
		cancel()
		os.Exit(0)
	}()

	cfg := Config{
		NumAccounts:   *accounts,
		Interval:      *interval,
		TableNum:      *tables,
		Concurrency:   *concurrency,
		EnableLongTxn: *longTxn,
	}
	bank := NewBankCase(&cfg)
	if err := bank.Initialize(ctx, db); err != nil {
		log.Fatalf("[bank] initial failed %v", err)
	}

	if err := bank.Execute(ctx, db); err != nil {
		log.Fatalf("[bank] returwith error %v", err)
	}
}
