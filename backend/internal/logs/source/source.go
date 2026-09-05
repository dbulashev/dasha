// Package source defines the log source contract: a provider streams records
// for one cluster and one stream, and declares how the fields of that stream
// map onto the roles Dasha shows. Filtering, masking, deduplication and
// pagination stay in the logs service, so every source behaves alike.
package source

import (
	"context"
	"errors"
	"time"

	"github.com/dbulashev/dasha/internal/config"
)

// Sentinel errors a provider returns; the logs service classifies them.
var (
	// ErrStream means the provider does not serve the requested stream.
	ErrStream = errors.New("stream not supported by log source")
	// ErrUnavailable means the source cannot serve this cluster at all.
	ErrUnavailable = errors.New("log source unavailable for cluster")
	// ErrConfig means the source is misconfigured: an unusable field map, a
	// missing index, credentials the store rejects.
	ErrConfig = errors.New("log source misconfigured")
	// ErrInvalidToken means the page token did not come from this source.
	ErrInvalidToken = errors.New("invalid page token")
	// ErrPartial means the source stopped before the range was exhausted and
	// cannot hand out a cursor to continue.
	ErrPartial = errors.New("log source stopped early")
)

// StreamPostgreSQL and StreamPooler are the wire values of the service_type
// API parameter.
const (
	StreamPostgreSQL = "postgresql"
	StreamPooler     = "pooler"
)

// Record is one log line: the raw field map of the stream plus the cursor that
// resumes reading right after it.
type Record struct {
	Timestamp time.Time
	Fields    map[string]string
	Token     string
}

// Filter is the part of a search a source may execute itself. Values are
// already validated: severities are canonical spellings from the field map,
// Host is one of the cluster's hosts. Pushing a filter down may only narrow
// the result — the authoritative filtering runs on the Dasha side.
type Filter struct {
	Severities []string
	Host       string
}

// StreamParams configures a single read.
type StreamParams struct {
	Cluster  config.Cluster
	Stream   string
	From, To time.Time
	Filter   Filter
	Token    string
}

// CheckResult reports whether a source is usable for a cluster and stream.
type CheckResult struct {
	// Target is the resolved upstream location: an index name, a cluster id.
	Target string
	// Documents counts records seen in the probe window.
	Documents int
	// Found maps field-map roles to the field names present upstream.
	Found map[string]string
	// Missing lists roles whose fields are absent upstream.
	Missing []string
	// Types maps a mapped field to the type the store indexes it as.
	Types map[string]string
	// Sample is one raw record; the caller masks it before it leaves the backend.
	Sample map[string]string
}

// Provider reads logs of one storage kind.
type Provider interface {
	// Streams lists the stream names this provider serves.
	Streams() []string
	// Fields describes the schema of a stream. The zero FieldMap means the
	// stream is unknown.
	Fields(stream string) FieldMap
	// Stream invokes fn for each record until it returns false, the range is
	// exhausted or ctx ends.
	Stream(ctx context.Context, p StreamParams, fn func(Record) bool) error
	// Check probes the source for a cluster and stream.
	Check(ctx context.Context, cluster config.Cluster, stream string) (CheckResult, error)
}

// ClusterMatcher is implemented by providers bound to clusters by their shape
// rather than by an explicit name in the configuration.
type ClusterMatcher interface {
	Matches(cluster config.Cluster) bool
}
