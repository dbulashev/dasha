package logs

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/dbulashev/dasha/internal/logs/source"
)

// What a scan asked the log store to filter on. Without it an empty answer
// cannot be told apart from a store that dropped the records itself.
const (
	NarrowedSeverity = "severity"
	NarrowedHost     = "host"
	NarrowedQueryID  = "query_id"
	NarrowedText     = "text"
	NarrowedDatabase = "database"
	NarrowedUser     = "user"
)

// narrowTimeout bounds what the provider spends deciding what it can filter
// itself: the metadata call of a store must not eat the budget of the scan.
const narrowTimeout = 5 * time.Second

// narrow asks the provider which parts of the filter it runs itself, on a
// budget of its own.
func narrow(ctx context.Context, p source.Provider, params source.StreamParams) source.Narrowed {
	ctx, cancel := context.WithTimeout(ctx, narrowTimeout)
	defer cancel()

	return p.Narrow(ctx, params)
}

// narrowedBy names each part of f: severity=<values>, host, query_id,
// text=<word> per word or term, text~<substring>, database, user.
func narrowedBy(f source.Filter) []string {
	var out []string

	if len(f.Severities) > 0 {
		out = append(out, NarrowedSeverity+"="+strings.Join(f.Severities, ","))
	}

	if f.Host != "" {
		out = append(out, NarrowedHost)
	}

	if f.QueryID != nil {
		out = append(out, NarrowedQueryID)
	}

	for _, w := range slices.Concat(f.Words, f.Terms) {
		out = append(out, NarrowedText+"="+w)
	}

	for _, s := range f.Substrings {
		out = append(out, NarrowedText+"~"+s)
	}

	if f.Database != "" {
		out = append(out, NarrowedDatabase)
	}

	if f.User != "" {
		out = append(out, NarrowedUser)
	}

	return out
}
