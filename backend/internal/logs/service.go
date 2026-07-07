// Package logs orchestrates Yandex Cloud cluster log search on top of the
// low-level StreamClusterLogs wrapper: it resolves the cluster to its folder
// SDK, builds an injection-safe native filter, applies Dasha-side filtering,
// masks sensitive text, and optionally deduplicates messages.
package logs

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/discovery/yandex"
	"github.com/dbulashev/dasha/internal/pkg/sanitize"
)

const defaultPageSize = 100

// Sentinel errors classified into HTTP status codes by the handler.
var (
	// ErrNotFound means the cluster name is unknown.
	ErrNotFound = errors.New("cluster not found")
	// ErrUnsupported means the cluster is not a Yandex MDB cluster or has no SDK.
	ErrUnsupported = errors.New("logs not supported for this cluster")
	// ErrInvalid means the request parameters failed validation.
	ErrInvalid = errors.New("invalid log search parameters")
	// ErrUpstream means the Yandex API returned an error.
	ErrUpstream = errors.New("yandex api error")
	// ErrTimeout means the upstream read exceeded the configured timeout.
	ErrTimeout = errors.New("yandex api timeout")
)

// ServiceTypePooler is the wire value selecting the connection pooler log.
const ServiceTypePooler = "pooler"

// ParseServiceType maps the API service_type string to a yandex.ServiceType,
// defaulting to PostgreSQL for any non-pooler value.
func ParseServiceType(s string) yandex.ServiceType {
	if s == ServiceTypePooler {
		return yandex.ServicePooler
	}

	return yandex.ServicePostgreSQL
}

// SearchQuery is a normalized log search request.
type SearchQuery struct {
	Cluster     string
	ServiceType yandex.ServiceType
	From, To    time.Time
	Severities  []string // native filter (allowlist)
	Host        string   // native filter (validated against cluster hosts)
	Message     string   // Dasha-side substring (case-insensitive)
	Database    string   // Dasha-side substring (case-insensitive)
	User        string   // Dasha-side substring (case-insensitive)
	Dedup       bool
	PageSize    int
	PageToken   string // non-dedup cursor only
}

// Entry is a single result row (or a dedup group when Count > 0).
type Entry struct {
	Timestamp time.Time
	Severity  string
	Hostname  string
	Text      string
	Database  string
	User      string
	Fields    map[string]string // full masked message map

	// Dedup-only fields.
	Count     int
	FirstSeen time.Time
	LastSeen  time.Time
}

// SearchResult is the outcome of a search.
type SearchResult struct {
	Items         []Entry
	NextPageToken string // present only when !Dedup and more records are available
	Dedup         bool
	Partial       bool // max_scan reached -> results/counts are incomplete
	Scanned       int
}

// Service searches cluster logs.
type Service interface {
	Search(ctx context.Context, q SearchQuery) (SearchResult, error)
}

type service struct {
	clusters config.Clusters
	registry *yandex.Registry
	cfg      config.LogSearchConfig
	logger   *zap.Logger
}

// NewService builds the log search service.
func NewService(
	clusters config.Clusters,
	registry *yandex.Registry,
	cfg config.LogSearchConfig,
	logger *zap.Logger,
) Service {
	return &service{
		clusters: clusters,
		registry: registry,
		cfg:      cfg.WithDefaults(),
		logger:   logger,
	}
}

func (s *service) Search(ctx context.Context, q SearchQuery) (SearchResult, error) {
	cluster, ok := s.findCluster(ctx, q.Cluster)
	if !ok {
		return SearchResult{}, ErrNotFound
	}

	if !cluster.SupportsLogs() {
		return SearchResult{}, fmt.Errorf("%w: cluster has no log source", ErrUnsupported)
	}

	sdk, ok := s.registry.Get(cluster.Labels["folder_id"])
	if !ok {
		return SearchResult{}, fmt.Errorf("%w: no SDK for folder", ErrUnsupported)
	}

	fd := fieldsFor(q.ServiceType)

	severities, err := s.validate(cluster, fd, q)
	if err != nil {
		return SearchResult{}, err
	}

	filter := buildFilter(fd, severities, q.Host)

	params := yandex.StreamLogsParams{ //nolint:exhaustruct
		ClusterID:   cluster.ProviderID,
		ServiceType: q.ServiceType,
		From:        q.From,
		To:          q.To,
		Filter:      filter,
		RecordToken: q.PageToken,
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	if q.Dedup {
		return s.searchDedup(ctx, sdk, params, q, fd)
	}

	return s.searchPage(ctx, sdk, params, q, fd)
}

func (s *service) findCluster(ctx context.Context, name string) (config.Cluster, bool) {
	clusters, err := s.clusters.Get(ctx)
	if err != nil {
		s.logger.Warn("logs: failed to list clusters", zap.Error(err))

		return config.Cluster{}, false //nolint:exhaustruct
	}

	for _, c := range clusters {
		if c.Name.String() == name {
			return c, true
		}
	}

	return config.Cluster{}, false //nolint:exhaustruct
}

// validate checks time range, severities and host; returns the severities in
// the casing the Yandex API expects.
func (s *service) validate(cluster config.Cluster, fd serviceFields, q SearchQuery) ([]string, error) {
	if !q.From.Before(q.To) {
		return nil, fmt.Errorf("%w: 'from' must be before 'to'", ErrInvalid)
	}

	// A resume cursor would make dedup counts cover an arbitrary partial
	// window and silently under-count.
	if q.Dedup && q.PageToken != "" {
		return nil, fmt.Errorf("%w: page_token cannot be combined with dedup", ErrInvalid)
	}

	severities := make([]string, 0, len(q.Severities))

	for _, raw := range q.Severities {
		if raw == "" {
			continue
		}

		v := fd.normalizeSeverity(raw)
		if _, ok := fd.severityAllow[v]; !ok {
			return nil, fmt.Errorf("%w: unknown severity %q", ErrInvalid, raw)
		}

		severities = append(severities, v)
	}

	if q.Host != "" && !hostInCluster(cluster, q.Host) {
		return nil, fmt.Errorf("%w: unknown host %q", ErrInvalid, q.Host)
	}

	return severities, nil
}

func hostInCluster(cluster config.Cluster, host string) bool {
	for _, h := range cluster.Hosts {
		if h.String() == host {
			return true
		}
	}

	return false
}

// buildFilter assembles the native StreamLogs filter from allowlisted values
// only (severity enum + validated host), so the expression is injection-safe.
func buildFilter(fd serviceFields, severities []string, host string) string {
	var parts []string

	if len(severities) > 0 {
		quoted := make([]string, len(severities))
		for i, sev := range severities {
			quoted[i] = `"` + sev + `"`
		}

		parts = append(parts, fmt.Sprintf("%s IN (%s)", fd.severityFilterField, strings.Join(quoted, ", ")))
	}

	if host != "" {
		parts = append(parts, fmt.Sprintf(`%s = "%s"`, fd.hostFilterField, host))
	}

	return strings.Join(parts, " AND ")
}

// searchPage collects up to PageSize matching records (cursor-based pagination).
// Once the page is full it keeps scanning (without consuming) until the next
// match or EOF, so NextPageToken is emitted only when more matches actually
// exist — never a token that leads to an empty page.
func (s *service) searchPage(
	ctx context.Context,
	sdk *yandex.SDK,
	params yandex.StreamLogsParams,
	q SearchQuery,
	fd serviceFields,
) (SearchResult, error) {
	pageSize := q.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}

	if pageSize > s.cfg.MaxPageSize {
		pageSize = s.cfg.MaxPageSize
	}

	var (
		items     = make([]Entry, 0, pageSize)
		scanned   int
		lastToken string
		hasMore   bool
		capped    bool
	)

	err := sdk.StreamLogs(ctx, params, func(rec yandex.LogRecord) bool {
		e, ok := s.toEntry(rec, q, fd)

		if ok && len(items) >= pageSize {
			// Lookahead match: do not consume it — the resume token must point
			// at the record before it so the next page returns this match.
			hasMore = true

			return false
		}

		scanned++
		lastToken = rec.Token

		if ok {
			items = append(items, e)
		}

		if scanned >= s.cfg.MaxScan {
			capped = true

			return false
		}

		return true
	})
	if err != nil {
		cErr := s.classify(ctx, err)
		if errors.Is(cErr, ErrTimeout) && len(items) > 0 {
			// Surface what was collected before the timeout as a partial page
			// instead of discarding it.
			return SearchResult{
				Items:         items,
				NextPageToken: lastToken,
				Dedup:         false,
				Partial:       true,
				Scanned:       scanned,
			}, nil
		}

		return SearchResult{}, cErr
	}

	// On a capped scan the token lets the client continue scanning even though
	// no further match has been seen yet.
	next := ""
	if hasMore || capped {
		next = lastToken
	}

	return SearchResult{
		Items:         items,
		NextPageToken: next,
		Dedup:         false,
		Partial:       capped,
		Scanned:       scanned,
	}, nil
}

// searchDedup scans up to MaxScan records and groups matches by normalized text.
func (s *service) searchDedup(
	ctx context.Context,
	sdk *yandex.SDK,
	params yandex.StreamLogsParams,
	q SearchQuery,
	fd serviceFields,
) (SearchResult, error) {
	var (
		groups  = make(map[string]*Entry)
		scanned int
		capped  bool
	)

	err := sdk.StreamLogs(ctx, params, func(rec yandex.LogRecord) bool {
		scanned++

		if e, ok := s.toEntry(rec, q, fd); ok {
			key := normalize(e.Text)

			if g, exists := groups[key]; exists {
				g.Count++

				if e.Timestamp.Before(g.FirstSeen) {
					g.FirstSeen = e.Timestamp
				}

				if e.Timestamp.After(g.LastSeen) {
					g.LastSeen = e.Timestamp
					g.Text = e.Text
					g.Fields = e.Fields
				}

				if severityRank(e.Severity) > severityRank(g.Severity) {
					g.Severity = e.Severity
				}
			} else {
				e.Count = 1
				e.FirstSeen = e.Timestamp
				e.LastSeen = e.Timestamp
				cp := e
				groups[key] = &cp
			}
		}

		if scanned >= s.cfg.MaxScan {
			capped = true

			return false
		}

		return true
	})
	if err != nil {
		cErr := s.classify(ctx, err)
		if !errors.Is(cErr, ErrTimeout) || len(groups) == 0 {
			return SearchResult{}, cErr
		}

		// Surface the groups collected before the timeout as a partial result
		// instead of discarding them.
		capped = true
	}

	items := make([]Entry, 0, len(groups))
	for _, g := range groups {
		items = append(items, *g)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}

		return items[i].LastSeen.After(items[j].LastSeen)
	})

	return SearchResult{
		Items:         items,
		NextPageToken: "",
		Dedup:         true,
		Partial:       capped,
		Scanned:       scanned,
	}, nil
}

// toEntry maps a raw record to an Entry, applying Dasha-side filters and masking.
// It returns ok=false when the record fails the message/database/user filters.
// Filters run against the raw values first so the map copy and masking are only
// paid for records that will actually be returned.
func (s *service) toEntry(
	rec yandex.LogRecord,
	q SearchQuery,
	fd serviceFields,
) (Entry, bool) {
	pf := fd.promoted

	if q.Message != "" && !containsFold(rec.Fields[pf.text], q.Message) {
		return Entry{}, false //nolint:exhaustruct
	}

	if q.Database != "" && !containsFold(rec.Fields[pf.database], q.Database) {
		return Entry{}, false //nolint:exhaustruct
	}

	if q.User != "" && !containsFold(rec.Fields[pf.user], q.User) {
		return Entry{}, false //nolint:exhaustruct
	}

	masked := make(map[string]string, len(rec.Fields))
	for k, v := range rec.Fields {
		masked[k] = v
	}

	for _, mk := range fd.mask {
		if v, ok := masked[mk]; ok {
			masked[mk] = sanitize.SQL(v)
		}
	}

	return Entry{ //nolint:exhaustruct
		Timestamp: rec.Timestamp,
		Severity:  masked[pf.severity],
		Hostname:  masked[pf.host],
		Text:      masked[pf.text],
		Database:  masked[pf.database],
		User:      masked[pf.user],
		Fields:    masked,
	}, true
}

// classify converts a low-level stream error into a sentinel error. A cancelled
// context due to the configured timeout maps to ErrTimeout, a client disconnect
// to context.Canceled; everything else to ErrUpstream (message sanitized of any
// embedded credentials).
func (s *service) classify(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrTimeout
	}

	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return context.Canceled
	}

	return fmt.Errorf("%w: %s", ErrUpstream, sanitize.SQL(err.Error()))
}
