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
// If useLongConn is true, it configures the connection pool for long connections:
// - Sets longer connection lifetime (1 hour)
// - Sets larger max open connections
// - Sets larger max idle connections
// If useLongConn is false (short connection mode):
// - Sets shorter connection lifetime (5 minutes)
// - Uses connection pool with limited connections
func OpenDB(dsn string, maxIdleConns int, useLongConn bool) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	if useLongConn {
		// Long connection mode: keep connections alive longer
		db.SetMaxOpenConns(maxIdleConns * 2) // Allow more open connections
		db.SetMaxIdleConns(maxIdleConns)
		db.SetConnMaxLifetime(1 * time.Hour) // Keep connections for 1 hour
		log.Info("DB opens successfully with long connection mode")
	} else {
		// Short connection mode: connections expire quickly
		db.SetMaxOpenConns(maxIdleConns)
		// Ensure at least 1 idle connection, but use fewer than maxOpenConns
		maxIdle := maxIdleConns / 2
		if maxIdle < 1 {
			maxIdle = 1
		}
		db.SetMaxIdleConns(maxIdle)
		db.SetConnMaxLifetime(5 * time.Minute) // Connections expire after 5 minutes
		log.Info("DB opens successfully with short connection mode")
	}

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

// MustExecWithConn executes SQL using dbConn interface (supports both *sql.DB and *sql.Conn)
func MustExecWithConn(dbConn interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}, query string, args ...interface{}) sql.Result {
	r, err := dbConn.Exec(query, args...)
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
