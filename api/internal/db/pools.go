package db

// M5: the API's database pool can be REPLACED while the app runs.
//
// With dynamic credentials the Postgres user changes every few minutes. Handlers ask
// Pools.Current() for the pool to use RIGHT NOW; the rotation code swaps in a pool for a
// new user before the old user expires, then closes the old pool gracefully (in-flight
// queries finish first).

import (
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolSource gives handlers the pool to use for this request.
type PoolSource interface {
	Current() *pgxpool.Pool
}

// active is one pool plus what we know about its credentials.
type active struct {
	pool  *pgxpool.Pool
	user  string
	since time.Time // when this pool (and its DB user) was put in service
}

// Pools holds the current pool and lets us swap it atomically.
type Pools struct {
	cur atomic.Pointer[active]
}

// Current returns the pool to use now.
func (p *Pools) Current() *pgxpool.Pool { return p.cur.Load().pool }

// Info reports which database user is in use and since when (for /status).
func (p *Pools) Info() (user string, since time.Time) {
	a := p.cur.Load()
	return a.user, a.since
}

// Swap installs a new pool and closes the previous one in the background.
func (p *Pools) Swap(pool *pgxpool.Pool, user string) {
	old := p.cur.Swap(&active{pool: pool, user: user, since: time.Now()})
	if old != nil {
		go old.pool.Close() // waits for borrowed connections to be returned
	}
}

// CloseAll closes the current pool (shutdown).
func (p *Pools) CloseAll() {
	if a := p.cur.Load(); a != nil {
		a.pool.Close()
	}
}
