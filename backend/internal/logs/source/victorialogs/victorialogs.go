// Package victorialogs reads logs from VictoriaLogs through LogsQL.
package victorialogs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
	"github.com/dbulashev/dasha/internal/logs/source/transport"
)

// checkWindow is the range Check counts records and samples one in.
const checkWindow = time.Hour

const (
	pathQuery      = "/select/logsql/query"
	pathFieldNames = "/select/logsql/field_names"
	pathHits       = "/select/logsql/hits"
)

// Provider serves one configured VictoriaLogs endpoint.
type Provider struct {
	client         *transport.Client
	streams        map[string]streamDef
	names          []string
	batchSize      int
	maxBoundaryIDs int
	timeout        time.Duration
	logger         *zap.Logger
}

// New validates the source configuration and builds its client. A field map
// that leaves a required role unbound fails here, at startup, rather than as an
// empty result later.
func New(cfg config.LogSourceConfig, global config.LogSearchConfig, logger *zap.Logger) (*Provider, error) {
	streams, err := streamsFromConfig(cfg)
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(global.TimeoutSeconds) * time.Second

	c, err := transport.New(cfg, timeout, transport.Options{
		NotFound: "endpoint not found; check the address and the VictoriaLogs version",
		Headers:  tenantHeaders(cfg.Tenant),
	})
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(streams))
	for name := range streams {
		names = append(names, name)
	}

	slices.Sort(names)

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = config.DefaultLogSourceBatchSize
	}

	// The skip list of a timestamp is read back in one request, so a cap below
	// the batch size would stop the read earlier than it says it does.
	maxBoundaryIDs := max(cfg.MaxBoundaryIDs, batchSize)
	if cfg.MaxBoundaryIDs <= 0 {
		maxBoundaryIDs = max(config.DefaultVictoriaLogsMaxBoundaryIDs, batchSize)
	}

	return &Provider{
		client:         c,
		streams:        streams,
		names:          names,
		batchSize:      batchSize,
		maxBoundaryIDs: maxBoundaryIDs,
		timeout:        timeout,
		logger:         logger,
	}, nil
}

func tenantHeaders(t config.LogSourceTenantConfig) map[string]string {
	if t.AccountID == 0 && t.ProjectID == 0 {
		return nil
	}

	return map[string]string{
		"AccountID": strconv.Itoa(t.AccountID),
		"ProjectID": strconv.Itoa(t.ProjectID),
	}
}

func (p *Provider) Streams() []string {
	return slices.Clone(p.names)
}

func (p *Provider) Fields(stream string) source.FieldMap {
	return p.streams[stream].fields
}

// Stream reads the range in batches, resuming from the cursor of the last
// record handed out. Reading runs from new to old: VictoriaLogs serves the N
// most recent records of a window cheaply, while sorting a whole window
// materializes it in the server.
func (p *Provider) Stream(ctx context.Context, sp source.StreamParams, fn func(source.Record) bool) error {
	def, ok := p.streams[sp.Stream]
	if !ok {
		return fmt.Errorf("%w: %q", source.ErrStream, sp.Stream)
	}

	d, err := def.expand(source.TemplateData{Cluster: sp.Cluster.Name.String()})
	if err != nil {
		return err
	}

	start, err := decodeCursor(sp.Token)
	if err != nil {
		return err
	}

	to := sp.To
	if !start.TS.IsZero() {
		to = start.TS
	}

	b := newBoundary(start)
	skipped := 0

	defer func() {
		if skipped > 0 {
			p.logger.Warn("victorialogs: records skipped, timestamp unusable",
				zap.String("target", d.target()),
				zap.String("field", d.fields.Timestamp),
				zap.Int("count", skipped))
		}
	}()

	for {
		// The skip list of a boundary timestamp is re-read on every request, so
		// the size has to reach past it for the read to make progress.
		size := min(len(b.hashes)+p.batchSize, p.maxBoundaryIDs)

		records, err := p.query(ctx, d.logsQL(sp.Filter, sp.From, to), size)
		if err != nil {
			return err
		}

		res, err := emit(records, d.fields, &b, p.maxBoundaryIDs, fn)
		skipped += res.skipped

		if err != nil {
			return err
		}

		if res.stop || len(records) < size {
			return nil
		}

		if res.delivered == 0 {
			if res.skipped == len(records) {
				return fmt.Errorf("%w: %d records in a row carry no usable %s",
					source.ErrPartial, res.skipped, d.fields.Timestamp)
			}

			return fmt.Errorf("%w: more than %d records share one timestamp; "+
				"the delivery agent writes time with too little precision",
				source.ErrPartial, p.maxBoundaryIDs)
		}

		to = b.ts
	}
}

// emitResult reports what one batch produced: records handed to fn, records
// dropped because their timestamp is unusable, and whether reading must stop.
type emitResult struct {
	delivered int
	skipped   int
	stop      bool
}

type entry struct {
	ts     time.Time
	hash   string
	fields map[string]string
}

// emit orders the batch from new to old — the order of an answer is not
// guaranteed — and hands the records not yet delivered to fn, giving each a
// cursor that resumes right after it.
func emit(
	records []map[string]string,
	fm source.FieldMap,
	b *boundary,
	maxHashes int,
	fn func(source.Record) bool,
) (emitResult, error) {
	var res emitResult

	entries := make([]entry, 0, len(records))

	for _, rec := range records {
		ts, err := source.ParseTime(rec[fm.Timestamp])
		if err != nil {
			// A record the delivery pipeline wrote without a usable timestamp
			// cannot be ordered or resumed from; the rest of the stream still can.
			res.skipped++

			continue
		}

		entries = append(entries, entry{ts: ts, hash: hashRecord(rec), fields: rec})
	}

	slices.SortStableFunc(entries, func(a, c entry) int { return c.ts.Compare(a.ts) })

	b.beginBatch()

	for _, e := range entries {
		if b.seen(e.ts, e.hash) {
			continue
		}

		if e.ts.Equal(b.ts) && len(b.hashes) >= maxHashes {
			res.stop = true

			return res, fmt.Errorf("%w: more than %d records share one timestamp; "+
				"the delivery agent writes time with too little precision", source.ErrPartial, maxHashes)
		}

		b.add(e.ts, e.hash)

		token, err := encodeCursor(cursor{TS: b.ts, Hashes: b.hashes})
		if err != nil {
			res.stop = true

			return res, err
		}

		if len(token) > maxTokenBytes {
			res.stop = true

			return res, fmt.Errorf("%w: the page token of one timestamp grew past %d bytes; "+
				"the delivery agent writes time with too little precision",
				source.ErrPartial, maxTokenBytes)
		}

		res.delivered++

		if !fn(source.Record{Timestamp: e.ts, Fields: e.fields, Token: token}) {
			res.stop = true

			return res, nil
		}
	}

	return res, nil
}

// Check reports whether the stream behind a cluster is reachable and carries
// the mapped fields. It is the only place a misspelled field name shows: a
// filter on a field VictoriaLogs does not hold returns an empty answer, not an
// error.
func (p *Provider) Check(ctx context.Context, cluster config.Cluster, stream string) (source.CheckResult, error) {
	def, ok := p.streams[stream]
	if !ok {
		return source.CheckResult{}, fmt.Errorf("%w: %q", source.ErrStream, stream)
	}

	d, err := def.expand(source.TemplateData{Cluster: cluster.Name.String()})
	if err != nil {
		return source.CheckResult{}, err
	}

	now := time.Now()
	expr := d.logsQL(source.Filter{Severities: nil, Host: ""}, now.Add(-checkWindow), now)

	res := source.CheckResult{
		Target:    d.target(),
		Documents: 0,
		Found:     map[string]string{},
		Missing:   nil,
		// VictoriaLogs holds every field as a string.
		Types:  map[string]string{},
		Sample: nil,
	}

	names, err := p.fieldNames(ctx, expr)
	if err != nil {
		return source.CheckResult{}, err
	}

	for role, field := range d.fields.Roles() {
		if _, found := names[field]; !found {
			res.Missing = append(res.Missing, role)

			continue
		}

		res.Found[role] = field
	}

	slices.Sort(res.Missing)

	res.Documents, err = p.hits(ctx, expr)
	if err != nil {
		return source.CheckResult{}, err
	}

	sample, err := p.query(ctx, expr, 1)
	if err != nil {
		return source.CheckResult{}, err
	}

	if len(sample) > 0 {
		res.Sample = sample[0]
	}

	return res, nil
}

// query runs one LogsQL read; limit bounds it to the most recent records of
// the window.
func (p *Provider) query(ctx context.Context, expr string, limit int) ([]map[string]string, error) {
	form := url.Values{}
	form.Set("query", expr)
	form.Set("limit", strconv.Itoa(limit))

	var out []map[string]string

	err := p.post(ctx, pathQuery, form, func(r io.Reader) error {
		records, err := readRecords(r, limit)
		out = records

		return err
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

func (p *Provider) fieldNames(ctx context.Context, expr string) (map[string]struct{}, error) {
	form := url.Values{}
	form.Set("query", expr)

	var resp fieldNamesResponse

	err := p.post(ctx, pathFieldNames, form, func(r io.Reader) error {
		if err := json.NewDecoder(r).Decode(&resp); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	out := make(map[string]struct{}, len(resp.Values))
	for _, v := range resp.Values {
		out[v.Value] = struct{}{}
	}

	return out, nil
}

func (p *Provider) hits(ctx context.Context, expr string) (int, error) {
	form := url.Values{}
	form.Set("query", expr)
	form.Set("step", checkWindow.String())

	var resp hitsResponse

	err := p.post(ctx, pathHits, form, func(r io.Reader) error {
		if err := json.NewDecoder(r).Decode(&resp); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	total := 0

	for _, h := range resp.Hits {
		for _, v := range h.Values {
			total += v
		}
	}

	return total, nil
}

// post sends one form-encoded select request carrying Dasha's own read
// timeout, which bounds the server-side query duration too.
func (p *Provider) post(ctx context.Context, path string, form url.Values, fn func(io.Reader) error) error {
	if p.timeout > 0 {
		form.Set("timeout", p.timeout.String())
	}

	return p.client.Do(ctx, transport.Request{
		Method:      http.MethodPost,
		Path:        path,
		ContentType: "application/x-www-form-urlencoded",
		Body:        []byte(form.Encode()),
	}, fn)
}
