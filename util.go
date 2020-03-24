package main

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/juju/errors"
	"github.com/ngaut/log"
)

// OpenDB opens db
func OpenDB(dsn string, maxIdleConns int) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxIdleConns(maxIdleConns)
	log.Info("DB opens successfully")
	return db, nil
}

// MustExec must execute sql or fatal
func MustExec(db *sql.DB, query string, args ...interface{}) sql.Result {
	r, err := db.Exec(query, args...)
	if err != nil {
		log.Fatalf("exec %s err %v", query, err)
	}
	return r
}

// RunWithRetry tries to run func in specified count
func RunWithRetry(ctx context.Context, retryCnt int, interval time.Duration, f func() error) error {
	var (
		err error
	)
	for i := 0; retryCnt < 0 || i < retryCnt; i++ {
		err = f()
		if err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
	return errors.Trace(err)
}
