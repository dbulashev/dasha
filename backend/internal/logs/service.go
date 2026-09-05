// Package logs orchestrates cluster log search on top of a log source: it
// resolves the cluster to its source, applies Dasha-side filtering, masks
// sensitive text, and optionally deduplicates messages.
package logs

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/config"
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
)

// SearchQuery is a normalized log search request.
type SearchQuery struct {
	Cluster    string
	Stream     string
	From, To   time.Time
	Severities []string // pushed down to the source (allowlist)
	Host       string   // pushed down to the source (validated against cluster hosts)
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
}

type service struct {
	clusters config.Clusters
	sources  *source.Registry
	cfg      config.LogSearchConfig
	logger   *zap.Logger
}

// NewService builds the log search service.
func NewService(
	clusters config.Clusters,
	sources *source.Registry,
	cfg config.LogSearchConfig,
	logger *zap.Logger,
) Service {
	return &service{
		clusters: clusters,
		sources:  sources,
		cfg:      cfg.WithDefaults(),
		logger:   logger,
	}
}

func (s *service) Search(ctx context.Context, q SearchQuery) (SearchResult, error) {
	cluster, ok := s.findCluster(ctx, q.Cluster)
	if !ok {
		return SearchResult{}, ErrNotFound
	}

	provider, sourceName, ok := s.sources.For(cluster)
	if !ok {
		return SearchResult{}, fmt.Errorf("%w: cluster has no log source", ErrUnsupported)
	}

	fm := provider.Fields(q.Stream)
	if fm.Empty() {
		return SearchResult{}, fmt.Errorf("%w: source %q has no stream %q", ErrUnsupported, sourceName, q.Stream)
	}

	severities, err := s.validate(cluster, fm, q)
	if err != nil {
		return SearchResult{}, err
	}

	user := ""
	if u := auth.UserFromContext(ctx); u != nil {
		user = u.Name
	}

	s.logger.Info("log search",
		zap.String("user", user),
		zap.String("cluster", q.Cluster),
		zap.String("source", sourceName),
		zap.String("service", q.Stream),
	)

	params := source.StreamParams{
		Cluster: cluster,
		Stream:  q.Stream,
		From:    q.From,
		To:      q.To,
		Filter: source.Filter{
			Severities: severities,
			Host:       q.Host,
		},
		Token: q.PageToken,
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	if q.Dedup {
		return s.searchDedup(ctx, provider, params, q, fm)
	}

	return s.searchPage(ctx, provider, params, q, fm)
}

// Check probes the source bound to the cluster and masks the sample record it
// brings back.
func (s *service) Check(ctx context.Context, cluster, stream string) (CheckReport, error) {
	c, ok := s.findCluster(ctx, cluster)
	if !ok {
		return CheckReport{}, ErrNotFound
	}

	provider, sourceName, ok := s.sources.For(c)
	if !ok {
		return CheckReport{}, fmt.Errorf("%w: cluster has no log source", ErrUnsupported)
	}

	fm := provider.Fields(stream)
	if fm.Empty() {
		return CheckReport{}, fmt.Errorf("%w: source %q has no stream %q", ErrUnsupported, sourceName, stream)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	res, err := provider.Check(ctx, c, stream)
	if err != nil {
		return CheckReport{}, s.classify(ctx, err)
	}

	report := CheckReport{
		Source:    sourceName,
		Stream:    stream,
		Target:    res.Target,
		Documents: res.Documents,
		Found:     res.Found,
		Missing:   res.Missing,
		Types:     res.Types,
		Sample:    nil,
	}

	if res.Sample != nil {
		report.Sample = maskFields(res.Sample, fm)
	}

	return report, nil
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

		v, ok := fm.CanonicalSeverity(raw)
		if !ok {
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
		scanned   int
		lastToken string
		hasMore   bool
		capped    bool
	)

	err := provider.Stream(ctx, params, func(rec source.Record) bool {
		e, ok := s.toEntry(rec, q, fm)

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

		// A timeout keeps its resume token; a source that stopped early cannot
		// hand one out. Either way what was collected is returned as a partial
		// page instead of being discarded.
		if errors.Is(cErr, ErrTimeout) && len(items) > 0 {
			return SearchResult{
				Items:         items,
				NextPageToken: lastToken,
				Dedup:         false,
				Partial:       true,
				Scanned:       scanned,
			}, nil
		}

		if errors.Is(err, source.ErrPartial) {
			return SearchResult{
				Items:         items,
				NextPageToken: "",
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
	provider source.Provider,
	params source.StreamParams,
	q SearchQuery,
	fm source.FieldMap,
) (SearchResult, error) {
	var (
		groups  = make(map[string]*Entry)
		scanned int
		capped  bool
	)

	err := provider.Stream(ctx, params, func(rec source.Record) bool {
		scanned++

		if e, ok := s.toEntry(rec, q, fm); ok {
			key := normalize(e.Text)

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
				e.Text = displayTemplate(e.Text)
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

		partial := errors.Is(err, source.ErrPartial) ||
			(errors.Is(cErr, ErrTimeout) && len(groups) > 0)
		if !partial {
			return SearchResult{}, cErr
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
		Scanned:       scanned,
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

		if strings.Contains(ex, displayPlaceholder) {
			if templated == "" {
				templated = displayTemplate(rec.Fields[fm.Text])
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

	return fmt.Errorf("%w: %s", ErrUpstream, sanitize.SQL(err.Error()))
}
