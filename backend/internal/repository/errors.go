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
