// Package db is the API's PostgreSQL layer.
//
// The ONE pattern to copy into a real codebase is WithTenant: every request runs its
// queries inside a transaction that first tells Postgres who is asking. RLS policies
// read those values. Handlers never write "WHERE company_id = ..." themselves.
package db

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Who is the person behind the current request. It comes from the verified JWT.
type Who struct {
	UserID    string `json:"user_id"`
	CompanyID string `json:"company_id"`
	Role      string `json:"role"` // "agent" | "supervisor"
	Name      string `json:"name"`
}

// Open creates a connection pool. addr is host:port (PgBouncer in the reference setup).
func Open(ctx context.Context, addr, dbName, user, password string) (*pgxpool.Pool, error) {
	dsn := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password), // escapes special characters safely
		Host:     addr,
		Path:     "/" + dbName,
		RawQuery: "sslmode=disable", // LAB ONLY: production uses sslmode=verify-full
	}
	cfg, err := pgxpool.ParseConfig(dsn.String())
	if err != nil {
		return nil, fmt.Errorf("parsing database config: %w", err)
	}
	// Behind PgBouncer in transaction mode, avoid pgx's named prepared-statement cache:
	// the next query may run on a different server connection. QueryExecModeExec uses
	// unnamed statements, which are always safe with poolers.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	cfg.MaxConns = 10

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connecting to %s as %s: %w", addr, user, err)
	}
	return pool, nil
}

// WithTenant runs fn inside a transaction where Postgres knows who is asking.
//
//	BEGIN
//	SELECT set_config('app.company_id', …, true), …   ← true = local to THIS transaction
//	… fn's queries (RLS filters them) …
//	COMMIT                                            ← settings disappear automatically
//
// Because the settings live only inside the transaction, this is safe with connection
// pools and PgBouncer: the next request on the same connection starts with nothing.
func WithTenant(ctx context.Context, pool *pgxpool.Pool, who Who, fn func(pgx.Tx) error) error {
	return pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`SELECT set_config('app.company_id', $1, true),
			        set_config('app.user_id',    $2, true),
			        set_config('app.role',       $3, true)`,
			who.CompanyID, who.UserID, who.Role)
		if err != nil {
			return fmt.Errorf("setting request context: %w", err)
		}
		return fn(tx)
	})
}

// IsRLSViolation reports whether err is Postgres refusing a row because of a policy
// ("new row violates row-level security policy", SQLSTATE 42501).
func IsRLSViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}

// ---- Queries ---------------------------------------------------------------------
// Note: NONE of these filter by company or user. Row Level Security does that.

// Conversation is one row as shown to the user.
type Conversation struct {
	ID           string `json:"id"`
	Company      string `json:"company"`
	AssignedTo   string `json:"assigned_to"` // name, "" if unassigned
	CustomerName string `json:"customer_name"`
	Channel      string `json:"channel"`
	Subject      string `json:"subject"`
	Status       string `json:"status"`
}

const listConversationsSQL = `
SELECT c.id::text, co.name, COALESCE(u.name, ''), c.customer_name, c.channel, c.subject, c.status
FROM support.conversations c
JOIN support.companies co ON co.id = c.company_id
LEFT JOIN support.users u ON u.id = c.assigned_to
ORDER BY co.name, c.created_at, c.customer_name`

// ListConversations returns whatever the policies allow the current request to see.
func ListConversations(ctx context.Context, q pgx.Tx) ([]Conversation, error) {
	rows, err := q.Query(ctx, listConversationsSQL)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Conversation, error) {
		var c Conversation
		err := r.Scan(&c.ID, &c.Company, &c.AssignedTo, &c.CustomerName, &c.Channel, &c.Subject, &c.Status)
		return c, err
	})
}

// UpdateStatus changes a conversation's status. Returns how many rows changed:
// 0 means "doesn't exist OR you're not allowed" — RLS makes those look the same.
func UpdateStatus(ctx context.Context, q pgx.Tx, id, status string) (int64, error) {
	tag, err := q.Exec(ctx, `UPDATE support.conversations SET status = $2 WHERE id = $1::uuid`, id, status)
	return tag.RowsAffected(), err
}

// CreateConversation inserts an unassigned conversation into companyID.
// Normally companyID is the caller's own company; the demo lets you try another one.
func CreateConversation(ctx context.Context, q pgx.Tx, companyID, customer, channel, subject string) error {
	_, err := q.Exec(ctx,
		`INSERT INTO support.conversations (company_id, customer_name, channel, subject)
		 VALUES ($1::uuid, $2, $3, $4)`, companyID, customer, channel, subject)
	return err
}

// ---- Directory (no RLS in this lab: users/companies are "directory" tables) -------

// DemoUser is a seeded user, shown in the demo login dropdown.
type DemoUser struct {
	Who
	Email   string `json:"email"`
	Company string `json:"company"`
}

const usersSQL = `
SELECT u.id::text, u.company_id::text, u.role, u.name, u.email, co.name
FROM support.users u JOIN support.companies co ON co.id = u.company_id`

// ListUsers returns all seeded users (demo login dropdown).
func ListUsers(ctx context.Context, pool *pgxpool.Pool) ([]DemoUser, error) {
	rows, err := pool.Query(ctx, usersSQL+` ORDER BY co.name, u.role DESC, u.name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanUser)
}

// FindUserByEmail looks up one user for the demo login.
func FindUserByEmail(ctx context.Context, pool *pgxpool.Pool, email string) (*DemoUser, error) {
	rows, err := pool.Query(ctx, usersSQL+` WHERE u.email = $1`, email)
	if err != nil {
		return nil, err
	}
	u, err := pgx.CollectExactlyOneRow(rows, scanUser)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func scanUser(r pgx.CollectableRow) (DemoUser, error) {
	var u DemoUser
	err := r.Scan(&u.UserID, &u.CompanyID, &u.Role, &u.Name, &u.Email, &u.Company)
	return u, err
}
