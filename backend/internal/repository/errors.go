package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// lockNotAvailableCode is the SQLSTATE PostgreSQL reports when lock_timeout fires.
	lockNotAvailableCode = "55P03"
	// queryCanceledCode is what a server-side statement_timeout reports.
	queryCanceledCode = "57014"
)

// SQLSTATEs that separate "this role may not look" and "this server has no such
// object" from a genuine failure.
const (
	insufficientPrivilegeCode = "42501"
	undefinedTableCode        = "42P01"
	undefinedColumnCode       = "42703"
	undefinedFunctionCode     = "42883"
	invalidSchemaNameCode     = "3F000"
	// objectNotInPrerequisiteStateCode is what a statistics library outside shared_preload_libraries reports.
	objectNotInPrerequisiteStateCode = "55000"
)

// IsTimeout reports whether err is a query that outran its deadline rather than
// a server fault — the caller can then answer with a timeout status instead of
// a generic 500. Either side may end it: Dasha's own context deadline, or a
// statement_timeout set on the instance.
func IsTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == queryCanceledCode
}

// IsLockTimeout reports whether PostgreSQL refused to wait for a lock, i.e. the
// object is held by another transaction (DDL, VACUUM FULL, an idle-in-transaction
// session).
func IsLockTimeout(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == lockNotAvailableCode
}
