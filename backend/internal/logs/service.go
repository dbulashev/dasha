// Package logs orchestrates cluster log search on top of a log source: it
// resolves the cluster to its source, applies Dasha-side filtering, masks
// sensitive text, and optionally deduplicates messages.
package logs

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/insights"
	"github.com/dbulashev/dasha/internal/logs/pattern"
	"github.com/dbulashev/dasha/internal/logs/source"
	"github.com/dbulashev/dasha/internal/pkg/sanitize"
)

const defaultPageSize = 100

// Sentinel errors classified into HTTP status codes by the handler.
var (
	// ErrNotFound means the cluster name is unknown.
	ErrNotFound = errors.New("cluster not found")
	// ErrUnsupported means no log source is bound to the cluster, or the
	// source does not serve the requested stream.
	ErrUnsupported = errors.New("logs not supported for this cluster")
	// ErrInvalid means the request parameters failed validation.
	ErrInvalid = errors.New("invalid log search parameters")
	// ErrUpstream means the log source returned an error.
	ErrUpstream = errors.New("log source error")
	// ErrTimeout means the upstream read exceeded the configured timeout.
	ErrTimeout = errors.New("log source timeout")
	// ErrDisabled means the feature is switched off in the configuration.
	ErrDisabled = errors.New("feature disabled")
	// ErrNoStorage means the request needs the snapshot storage Dasha is not
	// configured with.
	ErrNoStorage = errors.New("storage not configured")
)

// SearchQuery is a normalized log search request.
type SearchQuery struct {
	Cluster    string
	Stream     string
	From, To   time.Time
	Severities []string // pushed down to the source (allowlist)
	Host       string   // pushed down to the source (validated against cluster hosts)
	QueryID    *int64   // pushed down where the store can, checked here regardless
	Include    []string // Dasha-side substrings on message, all must match (AND)
	Exclude    []string // Dasha-side negative substrings on message (grep -v)
	Database   string   // Dasha-side substring (case-insensitive)
	User       string   // Dasha-side substring (case-insensitive)
	Dedup      bool
	PageSize   int
	PageToken  string // non-dedup cursor only
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

// CheckReport is the outcome of probing the source bound to a cluster.
type CheckReport struct {
	Source    string
	Stream    string
	Target    string
	Documents int
	Found     map[string]string
	Missing   []string
	Types     map[string]string
	Sample    map[string]string // masked
}

// Service searches cluster logs.
type Service interface {
	Search(ctx context.Context, q SearchQuery) (SearchResult, error)
	// Check probes the source bound to a cluster.
	Check(ctx context.Context, cluster, stream string) (CheckReport, error)
	// SourceName is the name of the source bound to a cluster, empty when none.
	SourceName(ctx context.Context, cluster string) string
	// Insights classifies every record of a window and summarizes its plans.
	Insights(ctx context.Context, q InsightsQuery) (ScanResult, error)
	// Plans reads a window for auto_explain records alone.
	Plans(ctx context.Context, q PlansQuery) (ScanResult, error)
	// PlansForQueryIDs reads a window for the plans of the given statements. It
	// backs the index advisor and is off unless index_advisor_evidence is set.
	PlansForQueryIDs(
		ctx context.Context, cluster, stream, database string, from, to time.Time, ids []int64,
	) (insights.PlanWindow, error)
	// Compare puts the plans of two windows side by side.
	Compare(ctx context.Context, q CompareQuery) (CompareResult, error)
	// Snapshot returns a stored scan; the three snapshot methods read the
	// storage only, never the log source.
	Snapshot(ctx context.Context, id uuid.UUID) (Scan, error)
	SnapshotGroups(ctx context.Context, q GroupsQuery) (GroupPage, error)
	SnapshotGroup(ctx context.Context, id uuid.UUID, ord int) (insights.PlanGroup, error)
}

type service struct {
	clusters  config.Clusters
	sources   *source.Registry
	cfg       config.LogSearchConfig
	insights  config.LogInsightsConfig
	snapshots SnapshotStore
	settings  ClusterSettings
	logger    *zap.Logger

	pendingMu sync.Mutex
	pending   map[uuid.UUID]chan struct{}
}

// NewService builds the log search service.
func NewService(
	clusters config.Clusters,
	sources *source.Registry,
	cfg config.LogSearchConfig,
	insightsCfg config.LogInsightsConfig,
	snapshots SnapshotStore,
	settings ClusterSettings,
	logger *zap.Logger,
) Service {
	return &service{ //nolint:exhaustruct
		clusters:  clusters,
		sources:   sources,
		cfg:       cfg.WithDefaults(),
		insights:  insightsCfg.WithDefaults(),
		snapshots: snapshots,
		settings:  settings,
		logger:    logger,
		pending:   make(map[uuid.UUID]chan struct{}),
	}
}

func (s *service) Search(ctx context.Context, q SearchQuery) (SearchResult, error) {
	b, err := s.resolve(ctx, q.Cluster, q.Stream)
	if err != nil {
		return SearchResult{}, err
	}

	severities, err := s.validate(b.cluster, b.fields, q)
	if err != nil {
		return SearchResult{}, err
	}

	s.logRead(ctx, "log search", q.Cluster, b.sourceName, q.Stream)

	params := source.StreamParams{
		Cluster: b.cluster,
		Stream:  q.Stream,
		From:    q.From,
		To:      q.To,
		Filter: source.Filter{ //nolint:exhaustruct
			Severities: severities,
			Host:       q.Host,
			QueryID:    q.QueryID,
		},
		Token: q.PageToken,
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	if q.Dedup {
		return s.searchDedup(ctx, b.provider, params, q, b.fields)
	}

	return s.searchPage(ctx, b.provider, params, q, b.fields)
}

// Check probes the source bound to the cluster and masks the sample record it
// brings back.
func (s *service) Check(ctx context.Context, cluster, stream string) (CheckReport, error) {
	b, err := s.resolve(ctx, cluster, stream)
	if err != nil {
		return CheckReport{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	res, err := b.provider.Check(ctx, b.cluster, stream)
	if err != nil {
		return CheckReport{}, s.classify(ctx, err)
	}

	report := CheckReport{
		Source:    b.sourceName,
		Stream:    stream,
		Target:    res.Target,
		Documents: res.Documents,
		Found:     res.Found,
		Missing:   res.Missing,
		Types:     res.Types,
		Sample:    nil,
	}

	if res.Sample != nil {
		report.Sample = maskFields(res.Sample, b.fields)
	}

	return report, nil
}

// binding is a cluster resolved to the source and the stream serving it.
type binding struct {
	cluster    config.Cluster
	provider   source.Provider
	sourceName string
	fields     source.FieldMap
}

func (s *service) resolve(ctx context.Context, cluster, stream string) (binding, error) {
	c, ok := s.findCluster(ctx, cluster)
	if !ok {
		return binding{}, ErrNotFound
	}

	provider, sourceName, ok := s.sources.For(c)
	if !ok {
		return binding{}, fmt.Errorf("%w: cluster has no log source", ErrUnsupported)
	}

	fm := provider.Fields(stream)
	if fm.Empty() {
		return binding{}, fmt.Errorf("%w: source %q has no stream %q", ErrUnsupported, sourceName, stream)
	}

	return binding{cluster: c, provider: provider, sourceName: sourceName, fields: fm}, nil
}

func (s *service) logRead(ctx context.Context, msg, cluster, sourceName, stream string) {
	user := ""
	if u := auth.UserFromContext(ctx); u != nil {
		user = u.Name
	}

	s.logger.Info(msg,
		zap.String("user", user),
		zap.String("cluster", cluster),
		zap.String("source", sourceName),
		zap.String("service", stream),
	)
}

// SourceName resolves the log source serving a cluster.
func (s *service) SourceName(ctx context.Context, cluster string) string {
	c, ok := s.findCluster(ctx, cluster)
	if !ok {
		return ""
	}

	_, name, ok := s.sources.For(c)
	if !ok {
		return ""
	}

	return name
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
// the casing the source stores them.
func (s *service) validate(cluster config.Cluster, fm source.FieldMap, q SearchQuery) ([]string, error) {
	if err := validateWindow(cluster, q.From, q.To, q.Host); err != nil {
		return nil, err
	}

	// A resume cursor would make dedup counts cover an arbitrary partial
	// window and silently under-count.
	if q.Dedup && q.PageToken != "" {
		return nil, fmt.Errorf("%w: page_token cannot be combined with dedup", ErrInvalid)
	}

	// A stream without the role would answer empty for every statement asked for.
	if q.QueryID != nil && fm.QueryID == "" {
		return nil, fmt.Errorf("%w: stream %q carries no query_id", ErrInvalid, q.Stream)
	}

	severities := make([]string, 0, len(q.Severities))

	for _, raw := range q.Severities {
		if raw == "" {
			continue
		}

		v, ok := fm.CanonicalSeverity(raw)
		if !ok {
			return nil, fmt.Errorf("%w: unknown severity %q", ErrInvalid, raw)
		}

		severities = append(severities, v)
	}

	return severities, nil
}

func validateWindow(cluster config.Cluster, from, to time.Time, host string) error {
	if !from.Before(to) {
		return fmt.Errorf("%w: 'from' must be before 'to'", ErrInvalid)
	}

	if host != "" && !hostInCluster(cluster, host) {
		return fmt.Errorf("%w: unknown host %q", ErrInvalid, host)
	}

	return nil
}

func hostInCluster(cluster config.Cluster, host string) bool {
	for _, h := range cluster.Hosts {
		if h.String() == host {
			return true
		}
	}

	return false
}

// searchPage collects up to PageSize matching records (cursor-based pagination).
// Once the page is full it keeps scanning (without consuming) until the next
// match or EOF, so NextPageToken is emitted only when more matches actually
// exist — never a token that leads to an empty page.
func (s *service) searchPage(
	ctx context.Context,
	provider source.Provider,
	params source.StreamParams,
	q SearchQuery,
	fm source.FieldMap,
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
		lastToken string
		hasMore   bool
	)

	st, err := s.scan(ctx, provider, params, s.searchLimits(), func(rec source.Record) bool {
		e, ok := s.toEntry(rec, q, fm)

		if ok && len(items) >= pageSize {
			// Lookahead match: do not consume it — the resume token must point
			// at the record before it so the next page returns this match.
			hasMore = true

			return false
		}

		lastToken = rec.Token

		if ok {
			items = append(items, e)
		}

		return true
	})
	if err != nil {
		// A timeout keeps its resume token; a source that stopped early cannot
		// hand one out. Either way what was collected is returned as a partial
		// page instead of being discarded.
		switch {
		case errors.Is(err, ErrTimeout) && len(items) > 0:
			return SearchResult{
				Items:         items,
				NextPageToken: lastToken,
				Dedup:         false,
				Partial:       true,
				Scanned:       st.Records,
			}, nil
		case st.Partial:
			return SearchResult{
				Items:         items,
				NextPageToken: "",
				Dedup:         false,
				Partial:       true,
				Scanned:       st.Records,
			}, nil
		default:
			return SearchResult{}, err
		}
	}

	// On a capped scan the token lets the client continue scanning even though
	// no further match has been seen yet.
	next := ""
	if hasMore || st.Capped {
		next = lastToken
	}

	return SearchResult{
		Items:         items,
		NextPageToken: next,
		Dedup:         false,
		Partial:       st.Capped,
		Scanned:       st.Records,
	}, nil
}

// searchDedup scans up to MaxScan records and groups matches by normalized text.
func (s *service) searchDedup(
	ctx context.Context,
	provider source.Provider,
	params source.StreamParams,
	q SearchQuery,
	fm source.FieldMap,
) (SearchResult, error) {
	groups := make(map[string]*Entry)

	st, err := s.scan(ctx, provider, params, s.searchLimits(), func(rec source.Record) bool {
		if e, ok := s.toEntry(rec, q, fm); ok {
			key := pattern.Key(e.Text)

			if g, exists := groups[key]; exists {
				g.Count++

				if e.Timestamp.Before(g.FirstSeen) {
					g.FirstSeen = e.Timestamp
				}

				if e.Timestamp.After(g.LastSeen) {
					g.LastSeen = e.Timestamp
					g.Fields = e.Fields
				}

				if severityRank(e.Severity) > severityRank(g.Severity) {
					g.Severity = e.Severity
				}
			} else {
				e.Count = 1
				e.FirstSeen = e.Timestamp
				e.LastSeen = e.Timestamp
				// The row shows the shared template (concrete values of one member
				// would mislead); the latest record's real values stay in Fields.
				e.Text = pattern.Display(e.Text)
				cp := e
				groups[key] = &cp
			}
		}

		return true
	})

	capped := st.Capped

	if err != nil {
		partial := st.Partial || (errors.Is(err, ErrTimeout) && len(groups) > 0)
		if !partial {
			return SearchResult{}, err
		}

		// Surface the groups collected before the source gave up as a partial
		// result instead of discarding them.
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
		Scanned:       st.Records,
	}, nil
}

// toEntry maps a raw record to an Entry, applying Dasha-side filters and masking.
// It returns ok=false when the record fails the message/database/user filters.
// Filters run against the raw values first so the map copy and masking happen
// only for records that will actually be returned.
func (s *service) toEntry(
	rec source.Record,
	q SearchQuery,
	fm source.FieldMap,
) (Entry, bool) {
	for _, inc := range q.Include {
		if inc != "" && !containsFold(rec.Fields[fm.Text], inc) {
			return Entry{}, false //nolint:exhaustruct
		}
	}

	// Excludes containing the display placeholder come from a dedup group row;
	// they only match the record's own masked template, never its raw text.
	templated := ""

	for _, ex := range q.Exclude {
		if ex == "" {
			continue
		}

		if strings.Contains(ex, pattern.Placeholder) {
			if templated == "" {
				templated = pattern.Display(rec.Fields[fm.Text])
			}

			if containsFold(templated, ex) {
				return Entry{}, false //nolint:exhaustruct
			}

			continue
		}

		if containsFold(rec.Fields[fm.Text], ex) {
			return Entry{}, false //nolint:exhaustruct
		}
	}

	if q.QueryID != nil && rec.Fields[fm.QueryID] != strconv.FormatInt(*q.QueryID, 10) {
		return Entry{}, false //nolint:exhaustruct
	}

	if q.Database != "" && !containsFold(rec.Fields[fm.Database], q.Database) {
		return Entry{}, false //nolint:exhaustruct
	}

	if q.User != "" && !containsFold(rec.Fields[fm.User], q.User) {
		return Entry{}, false //nolint:exhaustruct
	}

	masked := maskFields(rec.Fields, fm)

	return Entry{ //nolint:exhaustruct
		Timestamp: rec.Timestamp,
		Severity:  masked[fm.Severity],
		Hostname:  masked[fm.Host],
		Text:      masked[fm.Text],
		Database:  masked[fm.Database],
		User:      masked[fm.User],
		Fields:    masked,
	}, true
}

// maskFields copies the record's fields, passing the free-text ones listed in
// the field map through sanitize.SQL().
func maskFields(fields map[string]string, fm source.FieldMap) map[string]string {
	masked := make(map[string]string, len(fields))
	for k, v := range fields {
		masked[k] = v
	}

	for _, mk := range fm.Mask {
		if v, ok := masked[mk]; ok {
			masked[mk] = sanitize.SQL(v)
		}
	}

	return masked
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

	if errors.Is(err, source.ErrStream) || errors.Is(err, source.ErrUnavailable) {
		return fmt.Errorf("%w: %s", ErrUnsupported, err.Error())
	}

	if errors.Is(err, source.ErrInvalidToken) {
		return fmt.Errorf("%w: %s", ErrInvalid, err.Error())
	}

	s.logger.Warn("logs: source error", zap.Error(err))

	return fmt.Errorf("%w: %s", ErrUpstream, sanitize.SQL(err.Error()))
}
