package openbao

// M5: keeping credentials alive.
//
// Everything OpenBao hands out has a lifetime (TTL): the app's login token AND the dynamic
// database credentials. The official library's LifetimeWatcher renews a secret in the
// background until it can't be extended any further (max TTL reached). Then DoneCh fires,
// and it's our job to get a NEW one: log in again, or request new DB credentials.

import (
	"context"
	"fmt"
	"log"
	"time"

	bao "github.com/openbao/openbao/api/v2"
)

// KeepLoggedIn renews the app's token in the background; when it can't be renewed any
// more, it logs in again with AppRole. Runs until ctx is cancelled.
func (c *Client) KeepLoggedIn(ctx context.Context) {
	for {
		watcher, err := c.api.NewLifetimeWatcher(&bao.LifetimeWatcherInput{Secret: c.authSecret})
		if err != nil {
			log.Printf("[openbao] cannot watch token: %v", err)
			return
		}
		go watcher.Start()
		watch(ctx, watcher, "token", func(r *bao.RenewOutput) {
			if r.Secret != nil && r.Secret.Auth != nil {
				log.Printf("[openbao] token renewed, valid %ds more", r.Secret.Auth.LeaseDuration)
			}
		})
		watcher.Stop()
		if ctx.Err() != nil {
			return
		}
		// Re-login, retrying while OpenBao is unreachable (e.g. during a failover).
		for attempt := 1; ; attempt++ {
			loginCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.LoginAppRole(loginCtx, c.roleIDFile, c.secretIDFile)
			cancel()
			if err == nil {
				log.Printf("[openbao] token reached its max TTL → logged in again with AppRole")
				break
			}
			log.Printf("[openbao] re-login attempt %d failed: %v", attempt, err)
			if !sleep(ctx, backoff(attempt)) {
				return
			}
		}
	}
}

// Lease is something OpenBao handed out with a lifetime (here: DB credentials).
type Lease struct {
	secret *bao.Secret
	TTL    int // seconds, as first granted
}

// DynamicDBCreds asks the database engine for a brand-new Postgres user (M2/M5).
// Path: database/creds/<role>. The returned Lease is what we renew later.
func (c *Client) DynamicDBCreds(ctx context.Context, role string) (*Lease, *DBLogin, error) {
	s, err := c.api.Logical().ReadWithContext(ctx, "database/creds/"+role)
	if err != nil {
		return nil, nil, fmt.Errorf("requesting database credentials: %w", err)
	}
	if s == nil {
		return nil, nil, fmt.Errorf("no credentials returned for role %q", role)
	}
	var login DBLogin
	if login.Username, err = stringField(s.Data, "username"); err != nil {
		return nil, nil, err
	}
	if login.Password, err = stringField(s.Data, "password"); err != nil {
		return nil, nil, err
	}
	return &Lease{secret: s, TTL: s.LeaseDuration}, &login, nil
}

// WatchLease renews a lease (e.g. DB credentials) in the background and returns when it
// can no longer be extended. The caller should then fetch new credentials.
func (c *Client) WatchLease(ctx context.Context, lease *Lease, name string) error {
	watcher, err := c.api.NewLifetimeWatcher(&bao.LifetimeWatcherInput{Secret: lease.secret})
	if err != nil {
		return err
	}
	go watcher.Start()
	defer watcher.Stop()
	watch(ctx, watcher, name, func(r *bao.RenewOutput) {
		if r.Secret != nil {
			log.Printf("[openbao] %s lease renewed, valid %ds more", name, r.Secret.LeaseDuration)
		}
	})
	return ctx.Err()
}

// Leader returns the address of the cluster's current active node (for /status).
func (c *Client) Leader(ctx context.Context) (string, error) {
	l, err := c.api.Sys().LeaderWithContext(ctx)
	if err != nil {
		return "", err
	}
	return l.LeaderAddress, nil
}

// watch blocks until the watcher is done (or ctx ends), reporting each renewal.
func watch(ctx context.Context, w *bao.LifetimeWatcher, name string, onRenew func(*bao.RenewOutput)) {
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-w.DoneCh():
			if err != nil {
				log.Printf("[openbao] %s renewal stopped: %v", name, err)
			}
			return
		case r := <-w.RenewCh():
			onRenew(r)
		}
	}
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * time.Second
	if d > 10*time.Second {
		d = 10 * time.Second
	}
	return d
}

// sleep waits d, or returns false early if ctx ends.
func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
