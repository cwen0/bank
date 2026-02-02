package main

import (
	"database/sql"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/juju/errors"
	"github.com/ngaut/log"
	"golang.org/x/net/context"
)

// dbConn represents either a shared *sql.DB or a dedicated *sql.Conn for long connection mode
type dbConn interface {
	Begin() (*sql.Tx, error)
	Exec(query string, args ...interface{}) (sql.Result, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

// dbWrapper wraps *sql.DB to implement dbConn interface
type dbWrapper struct {
	db *sql.DB
}

func (w *dbWrapper) Begin() (*sql.Tx, error) {
	return w.db.Begin()
}

func (w *dbWrapper) Exec(query string, args ...interface{}) (sql.Result, error) {
	return w.db.Exec(query, args...)
}

func (w *dbWrapper) QueryRow(query string, args ...interface{}) *sql.Row {
	return w.db.QueryRow(query, args...)
}

func (w *dbWrapper) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return w.db.Query(query, args...)
}

// connWrapper wraps *sql.Conn to implement dbConn interface
type connWrapper struct {
	conn *sql.Conn
	ctx  context.Context
}

// newConnWrapper creates a new connWrapper with the given connection and context
func newConnWrapper(conn *sql.Conn, ctx context.Context) *connWrapper {
	return &connWrapper{
		conn: conn,
		ctx:  ctx,
	}
}

func (w *connWrapper) Begin() (*sql.Tx, error) {
	return w.conn.BeginTx(w.ctx, nil)
}

func (w *connWrapper) Exec(query string, args ...interface{}) (sql.Result, error) {
	return w.conn.ExecContext(w.ctx, query, args...)
}

func (w *connWrapper) QueryRow(query string, args ...interface{}) *sql.Row {
	return w.conn.QueryRowContext(w.ctx, query, args...)
}

func (w *connWrapper) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return w.conn.QueryContext(w.ctx, query, args...)
}

// BankCase is for concurrent balance transfer.
type BankCase struct {
	mu      sync.RWMutex
	cfg     *Config
	wg      sync.WaitGroup
	stopped int32
	// Cache for table index strings to avoid repeated fmt.Sprintf
	indexCache []string
}

// Config is config for bank test
type Config struct {
	// NumAccounts is total accounts
	NumAccounts      int           `toml:"num_accounts"`
	Interval         time.Duration `toml:"interval"`
	TableNum         int           `toml:"table_num"`
	Concurrency      int           `toml:"concurrency"`
	EnableLongTxn    bool          `toml:"enable_long_txn"`
	UseLongConn      bool          `toml:"use_long_conn"`       // If true, each goroutine maintains its own connection
	UseShortConnOnce bool          `toml:"use_short_conn_once"` // If true, open and close per operation
	RetryLimit       int           `toml:"retry_limit"`         // Retry count for operations
}

// NewBankCase returns the BankCase.
func NewBankCase(cfg *Config) *BankCase {
	b := &BankCase{
		cfg: cfg,
	}
	if b.cfg.TableNum <= 1 {
		b.cfg.TableNum = 1
	}
	// Pre-generate index strings to avoid repeated fmt.Sprintf
	b.indexCache = make([]string, b.cfg.TableNum)
	for i := 0; i < b.cfg.TableNum; i++ {
		if i > 0 {
			b.indexCache[i] = strconv.Itoa(i)
		} else {
			b.indexCache[i] = ""
		}
	}
	return b
}

// Initialize implements Case Initialize interface.
func (c *BankCase) Initialize(ctx context.Context, db *sql.DB) error {
	log.Infof("[%s] start to init...", c)
	defer func() {
		log.Infof("[%s] init end...", c)
	}()

	var dbConn dbConn
	if c.cfg.UseShortConnOnce {
		// One-shot mode: initDB will open/close per operation
		dbConn = nil
	} else if c.cfg.UseLongConn {
		// In long connection mode, use a dedicated connection for initialization
		conn, err := db.Conn(ctx)
		if err != nil {
			return errors.Trace(err)
		}
		defer conn.Close()
		dbConn = newConnWrapper(conn, ctx)
	} else {
		// In short connection pool mode, use shared connection pool
		dbConn = &dbWrapper{db: db}
	}

	for i := 0; i < c.cfg.TableNum; i++ {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		err := c.initDB(ctx, dbConn, db, i)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *BankCase) initDB(ctx context.Context, initConn dbConn, db *sql.DB, id int) error {
	// Use cached index string
	index := c.indexCache[id]

	var baseConn dbConn
	var cleanup func()
	if c.cfg.UseShortConnOnce {
		conn, err := db.Conn(ctx)
		if err != nil {
			return errors.Trace(err)
		}
		baseConn = newConnWrapper(conn, ctx)
		cleanup = func() { conn.Close() }
	} else {
		baseConn = initConn
		cleanup = func() {}
	}
	if baseConn == nil {
		return errors.New("init connection is nil")
	}
	defer cleanup()

	isDropped := c.tryDrop(baseConn, index)
	if !isDropped {
		c.startVerify(ctx, db, index)
		return nil
	}

	MustExecWithConn(baseConn, fmt.Sprintf("create table if not exists accounts%s (id BIGINT PRIMARY KEY, balance BIGINT NOT NULL, remark VARCHAR(128))", index))
	MustExecWithConn(baseConn, `create table if not exists record (id BIGINT AUTO_INCREMENT,
        from_id BIGINT NOT NULL,
        to_id BIGINT NOT NULL,
        from_balance BIGINT NOT NULL,
        to_balance BIGINT NOT NULL,
        amount BIGINT NOT NULL,
        tso BIGINT UNSIGNED NOT NULL,
        PRIMARY KEY(id))`)
	var wg sync.WaitGroup

	// TODO: fix the error is NumAccounts can't be divided by batchSize.
	// Insert batchSize values in one SQL.
	batchSize := 100
	jobCount := c.cfg.NumAccounts / batchSize

	maxLen := len(remark)
	ch := make(chan int, jobCount)
	for i := 0; i < c.cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// In long connection mode, each goroutine gets its own connection
			var workerConn dbConn
			if c.cfg.UseLongConn {
				conn, err := db.Conn(ctx)
				if err != nil {
					log.Fatalf("[%s] failed to get connection: %v", c, err)
					return
				}
				defer conn.Close()
				workerConn = newConnWrapper(conn, ctx)
			} else if !c.cfg.UseShortConnOnce {
				// In short connection pool mode, use shared connection
				workerConn = baseConn
			}

			// Use local random source to avoid lock contention on global rand
			rng := rand.New(rand.NewSource(time.Now().UnixNano()))
			var queryBuilder strings.Builder
			// Pre-allocate capacity to reduce allocations
			queryBuilder.Grow(batchSize * 50) // Estimate: ~50 chars per value

			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				startIndex, ok := <-ch
				if !ok {
					break
				}
				start := time.Now()

				// Build query efficiently using strings.Builder
				queryBuilder.Reset()
				queryBuilder.WriteString("INSERT IGNORE INTO accounts")
				queryBuilder.WriteString(index)
				queryBuilder.WriteString(" (id, balance, remark) VALUES ")

				for i := 0; i < batchSize; i++ {
					if i > 0 {
						queryBuilder.WriteByte(',')
					}
					queryBuilder.WriteByte('(')
					queryBuilder.WriteString(strconv.Itoa(startIndex + i))
					queryBuilder.WriteString(", 1000, \"")
					remarkLen := rng.Intn(maxLen)
					if remarkLen > 0 {
						queryBuilder.WriteString(remark[:remarkLen])
					}
					queryBuilder.WriteString("\")")
				}
				query := queryBuilder.String()
				insertF := func() error {
					if c.cfg.UseShortConnOnce {
						conn, err := db.Conn(ctx)
						if err != nil {
							return err
						}
						workerConn = newConnWrapper(conn, ctx)
						defer conn.Close()
					}
					_, err := workerConn.Exec(query)
					if IsErrDupEntry(err) {
						return nil
					}
					return err
				}
				err := RunWithRetry(ctx, c.cfg.RetryLimit, 5*time.Second, insertF)
				if err != nil {
					log.Fatalf("[%s]exec %s  err %s", c, query, err)
				}
				log.Infof("[%s] insert %d accounts%s, takes %s", c, batchSize, index, time.Since(start))
			}
		}()
	}

	// Send jobs to channel, but respect context cancellation
	go func() {
		defer close(ch)
		for i := 0; i < jobCount; i++ {
			select {
			case <-ctx.Done():
				return
			case ch <- i * batchSize:
			}
		}
	}()

	wg.Wait()

	select {
	case <-ctx.Done():
		log.Warn("[%s] bank initialize is cancel", c)
		return nil
	default:
	}

	c.startVerify(ctx, db, index)
	return nil
}

func (c *BankCase) startVerify(ctx context.Context, db *sql.DB, index string) {
	c.verify(ctx, db, index, noDelay)

	run := func(f func()) {
		ticker := time.NewTicker(c.cfg.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				f()
			}
		}
	}

	start := time.Now()
	// Start verify goroutine - it will exit when context is cancelled
	go run(func() {
		err := c.verify(ctx, db, index, noDelay)
		if err != nil {
			log.Infof("[%s] verify error: %s in: %s", c, err, time.Now())
			if time.Since(start) > defaultVerifyTimeout {
				atomic.StoreInt32(&c.stopped, 1)
				log.Infof("[%s] stop bank execute", c)
				// Note: We cannot call c.wg.Wait() here as it may cause deadlock
				// The Execute method's goroutines are still running and may be waiting
				// for stopped flag. Instead, we just set the flag and let Execute handle cleanup.
				log.Fatalf("[%s] verify timeout since %s, error: %s", c, start, err)
			}
		} else {
			start = time.Now()
			log.Infof("[%s] verify success in %s", c, time.Now())
		}
	})

	if c.cfg.EnableLongTxn {
		go run(func() { c.verify(ctx, db, index, delayRead) })
	}
}

// Execute implements Case Execute interface.
func (c *BankCase) Execute(ctx context.Context, db *sql.DB) error {
	log.Infof("[%s] start to test...", c)
	defer func() {
		log.Infof("[%s] test end...", c)
	}()
	var wg sync.WaitGroup

	run := func(f func(dbConn dbConn)) {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// In long connection mode, each goroutine gets its own connection
			var dbConn dbConn
			if c.cfg.UseLongConn {
				conn, err := db.Conn(ctx)
				if err != nil {
					log.Fatalf("[%s] failed to get connection: %v", c, err)
					return
				}
				defer conn.Close()
				dbConn = newConnWrapper(conn, ctx)
			} else if !c.cfg.UseShortConnOnce {
				// In short connection pool mode, use shared connection pool
				dbConn = &dbWrapper{db: db}
			}

			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				if atomic.LoadInt32(&c.stopped) != 0 {
					// too many log print in here if return error
					log.Errorf("[%s] bank stopped", c)
					return
				}
				c.wg.Add(1)
				// Use defer to ensure Done is called even if f panics
				func() {
					defer c.wg.Done()
					if c.cfg.UseShortConnOnce {
						conn, err := db.Conn(ctx)
						if err != nil {
							log.Fatalf("[%s] failed to get connection: %v", c, err)
							return
						}
						defer conn.Close()
						f(newConnWrapper(conn, ctx))
						return
					}
					f(dbConn)
				}()
			}
		}()
	}

	for i := 0; i < c.cfg.Concurrency; i++ {
		run(func(dbConn dbConn) { c.moveMoneyWithConn(ctx, dbConn, noDelay) })
	}
	if c.cfg.EnableLongTxn {
		run(func(dbConn dbConn) { c.moveMoneyWithConn(ctx, dbConn, delayRead) })
		run(func(dbConn dbConn) { c.moveMoneyWithConn(ctx, dbConn, delayCommit) })
	}

	wg.Wait()
	return nil
}

// String implements fmt.Stringer interface.
func (c *BankCase) String() string {
	return "bank"
}

// tryDrop will drop table if data incorrect and panic error likes Bad connect.
func (c *BankCase) tryDrop(dbConn dbConn, index string) bool {
	var (
		count int
		table string
	)
	//if table is not exist ,return true directly
	query := fmt.Sprintf("show tables like 'accounts%s'", index)
	err := dbConn.QueryRow(query).Scan(&table)
	switch {
	case err == sql.ErrNoRows:
		return true
	case err != nil:
		log.Fatalf("[%s] execute query %s error %v", c, query, err)
	}

	query = fmt.Sprintf("select count(*) as count from accounts%s", index)
	err = dbConn.QueryRow(query).Scan(&count)
	if err != nil {
		log.Fatalf("[%s] execute query %s error %v", c, query, err)
	}
	if count == c.cfg.NumAccounts {
		return false
	}

	log.Infof("[%s] we need %d accounts%s but got %d, re-initialize the data again", c, c.cfg.NumAccounts, index, count)
	MustExecWithConn(dbConn, fmt.Sprintf("drop table if exists accounts%s", index))
	MustExecWithConn(dbConn, "DROP TABLE IF EXISTS record")
	return true
}

func (c *BankCase) verify(ctx context.Context, db *sql.DB, index string, delay delayMode) error {
	// Get connection based on mode
	var dbConn dbConn
	if c.cfg.UseLongConn || c.cfg.UseShortConnOnce {
		conn, err := db.Conn(ctx)
		if err != nil {
			return errors.Trace(err)
		}
		defer conn.Close()
		dbConn = newConnWrapper(conn, ctx)
	} else {
		dbConn = &dbWrapper{db: db}
	}

	return c.verifyWithConn(ctx, dbConn, index, delay)
}

func (c *BankCase) verifyWithConn(ctx context.Context, dbConn dbConn, index string, delay delayMode) error {
	var total int

	tx, err := dbConn.Begin()
	if err != nil {
		return errors.Trace(err)
	}

	defer tx.Rollback()

	if delay == delayRead {
		err = c.delay(ctx)
		if err != nil {
			return err
		}
	}

	query := fmt.Sprintf("select sum(balance) as total from accounts%s", index)
	err = tx.QueryRow(query).Scan(&total)
	if err != nil {
		log.Errorf("[%s] select sum error %v", c, err)
		return errors.Trace(err)
	}
	if TiDBDatabase {
		var tso uint64
		if err = tx.QueryRow("select @@tidb_current_ts").Scan(&tso); err != nil {
			return errors.Trace(err)
		}
		log.Infof("[%s] select sum(balance) to verify use tso %d", c, tso)
	}
	tx.Commit()
	check := c.cfg.NumAccounts * 1000
	if total != check {
		log.Errorf("[%s] accouts%s total must %d, but got %d", c, index, check, total)
		atomic.StoreInt32(&c.stopped, 1)
		// Note: We cannot call c.wg.Wait() here as it may cause deadlock
		// The Execute method's goroutines are still running and may be waiting
		// for stopped flag. Instead, we just set the flag and let Execute handle cleanup.
		log.Fatalf("[%s] accouts%s total must %d, but got %d", c, index, check, total)
	}

	return nil
}

func (c *BankCase) moveMoneyWithConn(ctx context.Context, dbConn dbConn, delay delayMode) {
	// Use local random source to avoid lock contention on global rand
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	var (
		from, to, id int
	)
	for {
		from, to, id = rng.Intn(c.cfg.NumAccounts), rng.Intn(c.cfg.NumAccounts), rng.Intn(c.cfg.TableNum)
		if from == to {
			continue
		}
		break
	}
	// Use cached index string
	index := c.indexCache[id]

	amount := rng.Intn(999)

	err := c.execTransaction(ctx, dbConn, from, to, amount, index, delay)

	if err != nil {
		return
	}
}

func (c *BankCase) execTransaction(ctx context.Context, dbConn dbConn, from, to int, amount int, index string, delay delayMode) error {
	tx, err := dbConn.Begin()
	if err != nil {
		return errors.Trace(err)
	}

	defer tx.Rollback()

	if delay == delayRead {
		err = c.delay(ctx)
		if err != nil {
			return err
		}
	}

	// Build query using strings.Builder for better performance
	var queryBuilder strings.Builder
	queryBuilder.Grow(100)
	queryBuilder.WriteString("SELECT id, balance FROM accounts")
	queryBuilder.WriteString(index)
	queryBuilder.WriteString(" WHERE id IN (")
	queryBuilder.WriteString(strconv.Itoa(from))
	queryBuilder.WriteString(", ")
	queryBuilder.WriteString(strconv.Itoa(to))
	queryBuilder.WriteString(") FOR UPDATE")
	rows, err := tx.Query(queryBuilder.String())
	if err != nil {
		return errors.Trace(err)
	}
	defer rows.Close()

	var (
		fromBalance int
		toBalance   int
		count       int
	)

	for rows.Next() {
		var id, balance int
		if err = rows.Scan(&id, &balance); err != nil {
			return errors.Trace(err)
		}
		switch id {
		case from:
			fromBalance = balance
		case to:
			toBalance = balance
		default:
			log.Fatalf("[%s] got unexpected account %d", c, id)
		}

		count++
	}

	if err = rows.Err(); err != nil {
		return errors.Trace(err)
	}

	if count != 2 {
		log.Fatalf("[%s] select %d(%d) -> %d(%d) invalid count %d", c, from, fromBalance, to, toBalance, count)
	}

	var update string
	if fromBalance >= amount {
		// Build UPDATE query using strings.Builder for better performance
		var updateBuilder strings.Builder
		updateBuilder.Grow(200)
		updateBuilder.WriteString("UPDATE accounts")
		updateBuilder.WriteString(index)
		updateBuilder.WriteString(" SET balance = CASE id WHEN ")
		updateBuilder.WriteString(strconv.Itoa(to))
		updateBuilder.WriteString(" THEN ")
		updateBuilder.WriteString(strconv.Itoa(toBalance + amount))
		updateBuilder.WriteString(" WHEN ")
		updateBuilder.WriteString(strconv.Itoa(from))
		updateBuilder.WriteString(" THEN ")
		updateBuilder.WriteString(strconv.Itoa(fromBalance - amount))
		updateBuilder.WriteString(" END WHERE id IN (")
		updateBuilder.WriteString(strconv.Itoa(from))
		updateBuilder.WriteString(", ")
		updateBuilder.WriteString(strconv.Itoa(to))
		updateBuilder.WriteString(")")
		update = updateBuilder.String()
		_, err = tx.Exec(update)
		if err != nil {
			return errors.Trace(err)
		}

		var tso uint64
		if TiDBDatabase {
			if err = tx.QueryRow("select @@tidb_current_ts").Scan(&tso); err != nil {
				return err
			}
		} else {
			tso = uint64(time.Now().UnixNano())
		}
		// Build INSERT query using strings.Builder for better performance
		var insertBuilder strings.Builder
		insertBuilder.Grow(150)
		insertBuilder.WriteString("INSERT INTO record (from_id, to_id, from_balance, to_balance, amount, tso) VALUES (")
		insertBuilder.WriteString(strconv.Itoa(from))
		insertBuilder.WriteString(", ")
		insertBuilder.WriteString(strconv.Itoa(to))
		insertBuilder.WriteString(", ")
		insertBuilder.WriteString(strconv.Itoa(fromBalance))
		insertBuilder.WriteString(", ")
		insertBuilder.WriteString(strconv.Itoa(toBalance))
		insertBuilder.WriteString(", ")
		insertBuilder.WriteString(strconv.Itoa(amount))
		insertBuilder.WriteString(", ")
		insertBuilder.WriteString(strconv.FormatUint(tso, 10))
		insertBuilder.WriteString(")")
		if _, err = tx.Exec(insertBuilder.String()); err != nil {
			return err
		}
		log.Infof("[%s] exec pre: %s", c, update)
	}

	if delay == delayCommit {
		err = c.delay(ctx)
		if err != nil {
			return err
		}
	}

	err = tx.Commit()
	if fromBalance >= amount {
		if err != nil {
			log.Infof("[%s] exec commit error: %s\n err:%s", c, update, err)
		}
		if err == nil {
			log.Infof("[%s] exec commit success: %s", c, update)
		}
	}
	return err
}

func (c *BankCase) delay(ctx context.Context) error {
	start := time.Now()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	// Use local random source to avoid lock contention
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	delayDuration := minDelayDuration + time.Duration(rng.Int63n(int64(maxDelayDuration-minDelayDuration)))
	for {
		select {
		case <-ctx.Done():
			return errors.New("context canceled")
		case <-ticker.C:
			if atomic.LoadInt32(&c.stopped) != 0 {
				return errors.New("stopped")
			}
			if time.Since(start) > delayDuration {
				return nil
			}
		}
	}
}

type delayMode = int

const (
	noDelay delayMode = iota
	delayRead
	delayCommit
)

const (
	minDelayDuration = time.Minute*10 - time.Second*10
	maxDelayDuration = time.Minute*10 + time.Second*10
)
