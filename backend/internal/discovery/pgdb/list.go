// Package pgdb lists the databases of a PostgreSQL server for discovery providers.
package pgdb

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dbulashev/dasha/internal/version"
)

const (
	connectTimeout = 5 * time.Second
	queryTimeout   = 10 * time.Second
)

var errNoTargets = errors.New("no hosts to query")

// Target is one server to list databases on. Database is the bootstrap
// database the connection opens, not a database to monitor.
type Target struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

// Lister matches ListDatabases; tests substitute it.
type Lister func(ctx context.Context, t Target) ([]string, error)

// ListDatabases connects to t and lists the databases the role may connect to.
func ListDatabases(ctx context.Context, t Target) ([]string, error) {
	connCfg, err := pgx.ParseConfig(t.dsn())
	if err != nil {
		return nil, fmt.Errorf("parse connection config: %w", err)
	}

	connCfg.ConnectTimeout = connectTimeout
	// Same as the monitored pools: transaction-pooling proxies may sit in front.
	connCfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	if connCfg.RuntimeParams == nil {
		connCfg.RuntimeParams = make(map[string]string)
	}

	connCfg.RuntimeParams["application_name"] = fmt.Sprintf("dasha discovery %s %s",
		version.GetBuildNumber(), version.GetBuildDate())

	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	conn, err := pgx.ConnectConfig(queryCtx, connCfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	defer func() {
		// The query context may already be done.
		_ = conn.Close(context.WithoutCancel(ctx))
	}()

	rows, err := conn.Query(queryCtx, listDatabasesQuery)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}

	defer rows.Close()

	var names []string

	for rows.Next() {
		var name string

		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan datname: %w", err)
		}

		names = append(names, name)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}

	return names, nil
}

// ListFirst tries targets starting at start and returns the names and the index
// of the target that answered. The error, if every target failed, is the last
// one, prefixed with its host.
func ListFirst(ctx context.Context, list Lister, targets []Target, start int) ([]string, int, error) {
	if len(targets) == 0 {
		return nil, 0, errNoTargets
	}

	if start < 0 || start >= len(targets) {
		start = 0
	}

	var lastErr error

	for i := range targets {
		idx := (start + i) % len(targets)

		names, err := list(ctx, targets[idx])
		if err == nil {
			return names, idx, nil
		}

		lastErr = fmt.Errorf("%s: %w", targets[idx].Host, err)

		if ctx.Err() != nil {
			break
		}
	}

	return nil, start, lastErr
}

// dsn builds the connection string; url.URL escapes the credentials.
func (t Target) dsn() string {
	u := url.URL{ //nolint:exhaustruct
		Scheme: "postgres",
		User:   url.UserPassword(t.User, t.Password),
		Host:   net.JoinHostPort(t.Host, t.Port),
		Path:   "/" + t.Database,
	}

	return u.String()
}
