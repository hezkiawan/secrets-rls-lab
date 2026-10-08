// Package database opens connections to Postgres (through PgBouncer) and keeps
// track of which connection pool is the "current" one.
//
// Why "current"? Our DB user is temporary. Every few minutes the API gets a NEW user
// from OpenBao, opens a NEW pool with it, and replaces the old one (see SetPool).
package database

import (
	"context"
	"net/url"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The current pool, its DB username, and when we started using it.
// Two goroutines touch these (web requests read, the background job replaces),
// so we protect them with a mutex: only one goroutine at a time may use them.
var (
	mu          sync.Mutex
	currentPool *pgxpool.Pool
	currentUser string
	usingSince  time.Time
)

// OwnerPool is connected as the table OWNER. LAB ONLY: used by the "as owner" demo
// to show that the owner skips RLS. A real app never has this.
var OwnerPool *pgxpool.Pool

// Connect opens a connection pool to Postgres with the given username and password.
func Connect(address string, dbName string, username string, password string) (*pgxpool.Pool, error) {
	// Build "postgres://user:password@address/dbName". url.UserPassword escapes
	// special characters in the password safely.
	dbURL := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(username, password),
		Host:     address,
		Path:     "/" + dbName,
		RawQuery: "sslmode=disable", // LAB ONLY: production uses TLS
	}

	config, err := pgxpool.ParseConfig(dbURL.String())
	if err != nil {
		return nil, err
	}
	// PgBouncer (transaction pooling) and pgx's cached prepared statements don't mix.
	// This mode sends every query on its own, which always works with PgBouncer.
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	config.MaxConns = 10

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, err
	}

	// Check that the login really works.
	err = pool.Ping(context.Background())
	if err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// SetPool makes `pool` the current pool, then closes the previous one.
// It is called from the background job, so waiting here does not slow down requests.
func SetPool(pool *pgxpool.Pool, username string) {
	mu.Lock()
	oldPool := currentPool
	currentPool = pool
	currentUser = username
	usingSince = time.Now()
	mu.Unlock()

	if oldPool != nil {
		// A request may have picked up the old pool a moment ago. Give it time to
		// finish, then close the old pool (Close also waits for running queries).
		time.Sleep(5 * time.Second)
		oldPool.Close()
	}
}

// GetPool returns the current pool. Every repository function calls this.
func GetPool() *pgxpool.Pool {
	mu.Lock()
	defer mu.Unlock()
	return currentPool
}

// CurrentUser returns the DB username in use and since when (for /status).
func CurrentUser() (string, time.Time) {
	mu.Lock()
	defer mu.Unlock()
	return currentUser, usingSince
}
