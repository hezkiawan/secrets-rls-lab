package database

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"secrets-rls-lab/api/models"
)

// BeginWithTenant starts a transaction and tells Postgres WHO is asking.
// The RLS policies in reference/postgres/initdb/03-rls.sql read these 3 values.
//
//	BEGIN;
//	SELECT set_config('app.company_id', '...', true),
//	       set_config('app.user_id',    '...', true),
//	       set_config('app.role',       '...', true);
//	... your queries ...           <- RLS filters them
//	COMMIT;                        <- the 3 values disappear
//
// The `true` means "only for THIS transaction". That is what makes it safe with
// PgBouncer: the next request on the same connection starts with no values.
//
// The caller MUST finish the transaction with tx.Commit (and should `defer tx.Rollback`).
func BeginWithTenant(pool *pgxpool.Pool, user models.User) (pgx.Tx, error) {
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx,
		`SELECT set_config('app.company_id', $1, true),
		        set_config('app.user_id',    $2, true),
		        set_config('app.role',       $3, true)`,
		user.CompanyID, user.ID, user.Role)
	if err != nil {
		tx.Rollback(ctx)
		return nil, err
	}

	return tx, nil
}

// IsRLSViolation reports whether Postgres refused a row because of a policy.
// Postgres error code 42501 = "new row violates row-level security policy".
func IsRLSViolation(err error) bool {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		return pgError.Code == "42501"
	}
	return false
}
