package hotobjects

import (
	"slices"
	"sort"
	"time"
)

// Key builds the canonical map key for one object of one kind.
func Key(kind Kind, schema, object string) string {
	return string(kind) + ":" + schema + "." + object
}

// RankKey extracts the class ranking value from a counter set (cumulative or
// delta — the formula is the same).
func RankKey(kind Kind, class Class, c Counters) int64 {
	switch kind {
	case KindTable:
		switch class {
		case ClassReads:
			return c["seq_tup_read"] + c["idx_tup_fetch"]
		case ClassWrites:
			return c["n_tup_ins"] + c["n_tup_upd"] + c["n_tup_del"]
		case ClassIO:
			return c["heap_blks_read"] + c["idx_blks_read"] + c["toast_blks_read"]
		}
	case KindIndex:
		switch class {
		case ClassReads:
			return c["idx_tup_read"]
		case ClassIO:
			return c["idx_blks_read"]
		}
	}

	return 0
}

// ClassesFor returns the classes tracked for a kind.
func ClassesFor(kind Kind) []Class {
	if kind == KindIndex {
		return IndexClasses
	}

	return TableClasses
}

// Delta returns cur − prev. ok is false when any counter went backwards while
// the epoch looked intact — the object was dropped and recreated under the
// same name, so the interval is not measurable. A key absent from prev counts
// as zero (a new counter appearing after a PG upgrade).
func Delta(prev, cur Counters) (Counters, bool) {
	out := make(Counters, len(cur))

	for k, cv := range cur {
		pv := prev[k]
		if cv < pv {
			return nil, false
		}

		out[k] = cv - pv
	}

	return out, true
}

// HostSample is one host's raw capture: every object's cumulative counters
// plus the host's stats_reset epoch and role.
type HostSample struct {
	Instance   string
	CapturedAt time.Time
	StatsReset *time.Time
	InRecovery bool
	Rows       []AnchorRow
}

// BuildInput pairs a host's fresh sample with its stored anchors (nil on the
// very first run — no deltas can be computed then).
type BuildInput struct {
	Sample  HostSample
	Anchors map[string]AnchorRow
}

// aggObject accumulates one object's cluster-wide delta.
type aggObject struct {
	kind      Kind
	schema    string
	object    string
	tableName string
	sizeBytes int64
	delta     Counters
	perHost   map[string]HostDelta
}

// BuildSnapshot turns per-host samples+anchors into a stored snapshot:
// per-host epoch/regression checks, cluster-wide summing, per-class top-N,
// tail histograms and coverage. hostsMissing lists hosts that could not be
// sampled at all (they contribute nothing but mark the snapshot partial).
func BuildSnapshot(
	clusterName, database string,
	capturedAt time.Time,
	inputs []BuildInput,
	hostsMissing []string,
	topN int,
) Snapshot {
	snap := Snapshot{ //nolint:exhaustruct
		ClusterName:  clusterName,
		Database:     database,
		CapturedAt:   capturedAt,
		Windows:      make(map[string]HostWindow, len(inputs)),
		HostsMissing: hostsMissing,
		Coverage:     map[Class]KindRatio{},
		Histograms:   map[Class]KindHistogram{},
	}

	agg := map[string]*aggObject{}

	for _, in := range inputs {
		complete := epochIntact(in)

		from := capturedAt

		if len(in.Anchors) > 0 {
			for _, a := range in.Anchors {
				from = a.CapturedAt

				break
			}
		} else {
			complete = false // first run: nothing to delta against
		}

		snap.Windows[in.Sample.Instance] = HostWindow{
			From:       from,
			To:         in.Sample.CapturedAt,
			Complete:   complete,
			StatsReset: in.Sample.StatsReset,
		}

		for _, row := range in.Sample.Rows {
			k := Key(row.Kind, row.Schema, row.Object)

			o, ok := agg[k]
			if !ok {
				o = &aggObject{ //nolint:exhaustruct
					kind:      row.Kind,
					schema:    row.Schema,
					object:    row.Object,
					tableName: row.TableName,
					delta:     Counters{},
					perHost:   map[string]HostDelta{},
				}
				agg[k] = o
			}

			// What a DROP would reclaim is the largest copy across hosts.
			o.sizeBytes = max(o.sizeBytes, row.SizeBytes)

			if !complete {
				continue
			}

			anchor, ok := in.Anchors[k]
			if !ok {
				continue // new object on this host: no baseline yet
			}

			d, ok := Delta(anchor.Counters, row.Counters)
			if !ok {
				continue // recreated under the same name
			}

			for ck, cv := range d {
				o.delta[ck] += cv
			}

			o.perHost[in.Sample.Instance] = HostDelta{Complete: true, InRecovery: in.Sample.InRecovery, Delta: d}
		}
	}

	for _, kind := range []Kind{KindTable, KindIndex} {
		for _, class := range ClassesFor(kind) {
			snap.rankClass(kind, class, agg, topN)
		}
	}

	sortTop(snap.Top)

	return snap
}

// epochIntact reports whether the host's stats epoch matched its anchors.
// Anchors carry the stats_reset observed at their capture; any mismatch (reset
// happened, or reset time appeared/disappeared) breaks every delta of the host.
func epochIntact(in BuildInput) bool {
	if len(in.Anchors) == 0 {
		return false
	}

	var anchorReset *time.Time

	for _, a := range in.Anchors {
		anchorReset = a.StatsReset

		break
	}

	cur := in.Sample.StatsReset

	switch {
	case anchorReset == nil && cur == nil:
		return true
	case anchorReset == nil || cur == nil:
		return false
	default:
		return anchorReset.Equal(*cur)
	}
}

// rankClass fills one kind+class top and its tail histogram/coverage.
func (s *Snapshot) rankClass(kind Kind, class Class, agg map[string]*aggObject, topN int) {
	type ranked struct {
		obj *aggObject
		key int64
	}

	var all []ranked

	for _, o := range agg {
		if o.kind != kind || len(o.perHost) == 0 {
			continue
		}

		all = append(all, ranked{obj: o, key: RankKey(kind, class, o.delta)})
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].key != all[j].key {
			return all[i].key > all[j].key
		}
		// Deterministic order among equals.
		return all[i].obj.schema+all[i].obj.object < all[j].obj.schema+all[j].obj.object
	})

	n := min(topN, len(all))

	// Zero-activity objects never make the top: a rank among idle objects is
	// noise ("in the hot top" while doing nothing). On small schemas the top
	// would otherwise pad itself with silence — the demo lab's top-7 over 17
	// mostly-idle tables showed exactly that. all is sorted descending, so
	// trimming from the boundary is enough; the zeros land in the tail
	// histogram, where the idle mass is meaningful.
	for n > 0 && all[n-1].key <= 0 {
		n--
	}

	var sumTop, sumTail, maxTail int64

	tailKeys := make([]float64, 0, len(all)-n)

	for i, r := range all {
		if i < n {
			sumTop += r.key

			s.Top = append(s.Top, TopEntry{
				Rank:      i + 1,
				Kind:      kind,
				Class:     class,
				Schema:    r.obj.schema,
				Object:    r.obj.object,
				TableName: r.obj.tableName,
				SizeBytes: r.obj.sizeBytes,
				Delta:     r.obj.delta,
				PerHost:   r.obj.perHost,
			})

			continue
		}

		sumTail += r.key
		maxTail = max(maxTail, r.key)
		tailKeys = append(tailKeys, float64(r.key))
	}

	hist := &Histogram{ //nolint:exhaustruct
		Deciles: deciles(tailKeys),
		Count:   len(tailKeys),
		Sum:     sumTail,
		Max:     maxTail,
	}

	cov := 1.0
	if sumTop+sumTail > 0 {
		cov = float64(sumTop) / float64(sumTop+sumTail)
	}

	kh := s.Histograms[class]
	kr := s.Coverage[class]

	if kind == KindTable {
		kh.Tables = hist
		kr.Tables = cov
	} else {
		kh.Indexes = hist
		kr.Indexes = cov
	}

	s.Histograms[class] = kh
	s.Coverage[class] = kr
}

// deciles returns the 10th..90th percentiles (nearest-rank) of vals; vals may
// arrive in any order. Empty input yields an empty slice.
func deciles(vals []float64) []float64 {
	if len(vals) == 0 {
		return []float64{}
	}

	sorted := slices.Clone(vals)
	sort.Float64s(sorted)

	out := make([]float64, 0, 9)

	for p := 1; p <= 9; p++ {
		idx := (p*len(sorted) + 9) / 10 // ceil(p/10 * n)
		if idx > len(sorted) {
			idx = len(sorted)
		}

		out = append(out, sorted[idx-1])
	}

	return out
}

// Percentile projects a value onto a stored histogram plus the top boundary:
// the share of tail objects the value STRICTLY exceeds, 0..0.9 (values above
// every decile cap at 0.9; callers combine with the top membership check for
// the exact upper range). Strict comparison matters on quiet databases: a
// mostly-idle tail has all-zero deciles, and v >= d would grant an idle
// object (v=0) the full 0.9 — "hotter than 90% of tables" while doing nothing.
func (h *Histogram) Percentile(v float64) float64 {
	if h == nil || len(h.Deciles) == 0 || v <= 0 {
		return 0
	}

	p := 0.0

	for i, d := range h.Deciles {
		if v > d {
			p = float64(i+1) / 10.0
		}
	}

	return p
}

// sortTop orders entries kind, class, rank for stable storage and output.
func sortTop(top []TopEntry) {
	sort.Slice(top, func(i, j int) bool {
		if top[i].Kind != top[j].Kind {
			return top[i].Kind < top[j].Kind
		}

		if top[i].Class != top[j].Class {
			return top[i].Class < top[j].Class
		}

		return top[i].Rank < top[j].Rank
	})
}
