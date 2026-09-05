// Package opensearch reads logs from an OpenSearch or Elasticsearch index.
package opensearch

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

// checkWindow is the range Check counts documents and samples a record in.
const checkWindow = time.Hour

type streamDef struct {
	index    string
	selector map[string]string
	fields   source.FieldMap
}

// Provider serves one configured log store.
type Provider struct {
	client         *client
	streams        map[string]streamDef
	names          []string
	batchSize      int
	maxBoundaryIDs int
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

	c, err := newClient(cfg, time.Duration(global.TimeoutSeconds)*time.Second)
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
		maxBoundaryIDs = max(config.DefaultLogSourceMaxBoundaryIDs, batchSize)
	}

	return &Provider{
		client:         c,
		streams:        streams,
		names:          names,
		batchSize:      batchSize,
		maxBoundaryIDs: maxBoundaryIDs,
		logger:         logger,
	}, nil
}

func (p *Provider) Streams() []string {
	return slices.Clone(p.names)
}

func (p *Provider) Fields(stream string) source.FieldMap {
	return p.streams[stream].fields
}

// Stream reads the range in batches, resuming from the cursor of the last
// record handed out.
func (p *Provider) Stream(ctx context.Context, sp source.StreamParams, fn func(source.Record) bool) error {
	def, ok := p.streams[sp.Stream]
	if !ok {
		return fmt.Errorf("%w: %q", source.ErrStream, sp.Stream)
	}

	data := templateData{Cluster: sp.Cluster.Name.String()}

	index, err := expandIndex(def.index, data)
	if err != nil {
		return err
	}

	selector, err := expandSelector(def.selector, data)
	if err != nil {
		return err
	}

	start, err := decodeCursor(sp.Token)
	if err != nil {
		return err
	}

	from := sp.From
	if !start.TS.IsZero() {
		from = start.TS
	}

	b := newBoundary(start)
	skipped := 0

	defer func() {
		if skipped > 0 {
			p.logger.Warn("opensearch: records skipped, timestamp unusable",
				zap.String("index", index),
				zap.String("field", def.fields.Timestamp),
				zap.Int("count", skipped))
		}
	}()

	for {
		// The skip list of a boundary timestamp is re-read on every request, so
		// the size has to reach past it for the read to make progress.
		size := min(len(b.ids)+p.batchSize, p.maxBoundaryIDs)

		req := buildSearch(def.fields, selector, sp.Filter, from, sp.To, size, false)

		var resp searchResponse
		if err := p.client.call(ctx, "POST", "/"+url.PathEscape(index)+"/_search", req, &resp); err != nil {
			return err
		}

		res, err := emit(resp.Hits.Hits, def.fields, &b, fn)
		skipped += res.skipped

		if err != nil {
			return err
		}

		if res.stop || len(resp.Hits.Hits) < size {
			return nil
		}

		if res.delivered == 0 {
			if res.skipped == len(resp.Hits.Hits) {
				return fmt.Errorf("%w: %d records in a row carry no usable %s",
					source.ErrPartial, res.skipped, def.fields.Timestamp)
			}

			return fmt.Errorf("%w: more than %d records share one timestamp",
				source.ErrPartial, p.maxBoundaryIDs)
		}

		from = b.ts
	}
}

// emitResult reports what one batch produced: records handed to fn, records
// dropped because their timestamp is unusable, and whether reading must stop.
type emitResult struct {
	delivered int
	skipped   int
	stop      bool
}

// emit hands the hits not yet delivered to fn, giving each a cursor that
// resumes right after it.
func emit(
	hits []hit,
	fm source.FieldMap,
	b *boundary,
	fn func(source.Record) bool,
) (emitResult, error) {
	var res emitResult

	for _, h := range hits {
		fields := make(map[string]string, len(h.Source))
		flatten("", h.Source, fields)

		raw, ok := h.Source[fm.Timestamp]
		if !ok {
			if v, nested := fields[fm.Timestamp]; nested {
				raw = v
			}
		}

		ts, err := parseTime(raw)
		if err != nil {
			// A document the delivery pipeline wrote without a usable timestamp
			// cannot be ordered or resumed from; the rest of the index still can.
			res.skipped++

			continue
		}

		if b.seen(ts, h.ID) {
			continue
		}

		b.add(ts, h.ID)

		token, err := encodeCursor(cursor{TS: b.ts, IDs: b.ids})
		if err != nil {
			res.stop = true

			return res, err
		}

		res.delivered++

		if !fn(source.Record{Timestamp: ts, Fields: fields, Token: token}) {
			res.stop = true

			return res, nil
		}
	}

	return res, nil
}

// Check reports whether the index behind a stream is reachable and carries the
// mapped fields.
func (p *Provider) Check(ctx context.Context, cluster config.Cluster, stream string) (source.CheckResult, error) {
	def, ok := p.streams[stream]
	if !ok {
		return source.CheckResult{}, fmt.Errorf("%w: %q", source.ErrStream, stream)
	}

	data := templateData{Cluster: cluster.Name.String()}

	index, err := expandIndex(def.index, data)
	if err != nil {
		return source.CheckResult{}, err
	}

	selector, err := expandSelector(def.selector, data)
	if err != nil {
		return source.CheckResult{}, err
	}

	res := source.CheckResult{
		Target:    index,
		Documents: 0,
		Found:     map[string]string{},
		Missing:   nil,
		Sample:    nil,
		Types:     map[string]string{},
	}

	var caps fieldCapsResponse
	if err := p.client.call(ctx, "GET",
		"/"+url.PathEscape(index)+"/_field_caps?fields=*", nil, &caps); err != nil {
		return source.CheckResult{}, err
	}

	for role, field := range def.fields.Roles() {
		types, ok := caps.Fields[field]
		if !ok {
			res.Missing = append(res.Missing, role)

			continue
		}

		res.Found[role] = field

		names := make([]string, 0, len(types))
		for name := range types {
			names = append(names, name)
		}

		slices.Sort(names)
		res.Types[field] = strings.Join(names, ",")
	}

	slices.Sort(res.Missing)

	now := time.Now()
	req := buildSearch(def.fields, selector, source.Filter{Severities: nil, Host: ""},
		now.Add(-checkWindow), now, 1, true)

	var resp searchResponse
	if err := p.client.call(ctx, "POST", "/"+url.PathEscape(index)+"/_search", req, &resp); err != nil {
		return source.CheckResult{}, err
	}

	res.Documents = resp.Hits.Total.Value

	if len(resp.Hits.Hits) > 0 {
		sample := make(map[string]string, len(resp.Hits.Hits[0].Source))
		flatten("", resp.Hits.Hits[0].Source, sample)
		res.Sample = sample
	}

	return res, nil
}

// boundary tracks the records already delivered at the timestamp reading
// resumes from.
type boundary struct {
	ts  time.Time
	ids []string
	set map[string]struct{}
}

func newBoundary(c cursor) boundary {
	b := boundary{ts: c.TS, ids: c.IDs, set: make(map[string]struct{}, len(c.IDs))}
	for _, id := range c.IDs {
		b.set[id] = struct{}{}
	}

	return b
}

func (b *boundary) seen(ts time.Time, id string) bool {
	if !ts.Equal(b.ts) {
		return false
	}

	_, ok := b.set[id]

	return ok
}

func (b *boundary) add(ts time.Time, id string) {
	if !ts.Equal(b.ts) {
		b.ts = ts
		b.ids = nil
		b.set = map[string]struct{}{}
	}

	b.ids = append(b.ids, id)
	b.set[id] = struct{}{}
}
