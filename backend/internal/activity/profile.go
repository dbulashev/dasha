package activity

import (
	"cmp"
	"slices"
	"time"
)

// Filter narrows the sessions a profile counts. Empty fields match everything.
type Filter struct {
	Database          string
	Class             string
	IncludeBackground bool
}

func (f Filter) match(s Session) bool {
	if !Counted(s, f.IncludeBackground) {
		return false
	}

	if f.Database != "" && s.Database != f.Database {
		return false
	}

	return f.Class == "" || Class(s) == f.Class
}

// AAS is average active sessions: session count over successful samples.
type Share struct {
	Value    string
	AAS      float64
	SharePct float64
}

type EventShare struct {
	Class     string
	WaitEvent string
	AAS       float64
	SharePct  float64
}

type QueryShare struct {
	Key          string
	KeyKind      string
	QueryID      int64
	AAS          float64
	SharePct     float64
	ByClass      map[string]float64 // share_pct of the query's own sessions
	DistinctPIDs int
	Text         string
}

type Profile struct {
	From, To   time.Time
	Samples    int
	Truncated  int
	AAS        float64
	ByClass    []Share
	TopEvents  []EventShare
	TopQueries []QueryShare
	ByDatabase []Share
	ByUser     []Share
	ByApp      []Share
	ByBackend  []Share
}

type eventKey struct{ class, event string }

type queryAcc struct {
	kind    string
	queryID int64
	n       int
	byClass map[string]int
	pids    map[int32]struct{}
	text    string
}

// Aggregate folds successful samples into a profile; top bounds every list but ByClass.
func Aggregate(samples []Sample, f Filter, top int) Profile {
	p := Profile{Samples: len(samples)}
	if len(samples) == 0 {
		return p
	}

	var (
		total   int
		classes = map[string]int{}
		events  = map[eventKey]int{}
		queries = map[string]*queryAcc{}
		dbs     = map[string]int{}
		users   = map[string]int{}
		apps    = map[string]int{}
		btypes  = map[string]int{}
	)

	for _, smp := range samples {
		if p.From.IsZero() || smp.At.Before(p.From) {
			p.From = smp.At
		}

		if smp.At.After(p.To) {
			p.To = smp.At
		}

		if smp.Truncated {
			p.Truncated++
		}

		for _, s := range smp.Sessions {
			if !f.match(s) {
				continue
			}

			cls := Class(s)
			total++
			classes[cls]++
			events[eventKey{class: cls, event: s.WaitEvent}]++
			dbs[s.Database]++
			users[s.User]++
			apps[s.Application]++
			btypes[s.BackendType]++

			if !RunsStatement(s.BackendType) {
				continue
			}

			q, ok := queries[s.Key]
			if !ok {
				q = &queryAcc{kind: s.KeyKind, queryID: s.QueryID, byClass: map[string]int{}, pids: map[int32]struct{}{}}
				queries[s.Key] = q
			}

			q.n++
			q.byClass[cls]++
			q.pids[s.PID] = struct{}{}

			if q.text == "" {
				q.text = s.Text
			}
		}
	}

	n := float64(len(samples))
	aas := func(c int) float64 { return float64(c) / n }
	pct := func(c int) float64 {
		if total == 0 {
			return 0
		}

		return float64(c) * 100 / float64(total)
	}

	p.AAS = aas(total)

	for _, c := range Classes {
		if classes[c] > 0 {
			p.ByClass = append(p.ByClass, Share{Value: c, AAS: aas(classes[c]), SharePct: pct(classes[c])})
		}
	}

	slices.SortStableFunc(p.ByClass, func(a, b Share) int { return cmp.Compare(b.AAS, a.AAS) })

	for k, c := range events {
		p.TopEvents = append(p.TopEvents, EventShare{Class: k.class, WaitEvent: k.event, AAS: aas(c), SharePct: pct(c)})
	}

	slices.SortFunc(p.TopEvents, func(a, b EventShare) int {
		return cmp.Or(cmp.Compare(b.AAS, a.AAS), cmp.Compare(a.Class, b.Class), cmp.Compare(a.WaitEvent, b.WaitEvent))
	})

	for key, q := range queries {
		byClass := make(map[string]float64, len(q.byClass))
		for c, v := range q.byClass {
			byClass[c] = float64(v) * 100 / float64(q.n)
		}

		p.TopQueries = append(p.TopQueries, QueryShare{
			Key: key, KeyKind: q.kind, QueryID: q.queryID, AAS: aas(q.n), SharePct: pct(q.n),
			ByClass: byClass, DistinctPIDs: len(q.pids), Text: q.text,
		})
	}

	slices.SortFunc(p.TopQueries, func(a, b QueryShare) int {
		return cmp.Or(cmp.Compare(b.AAS, a.AAS), cmp.Compare(a.Key, b.Key))
	})

	p.TopEvents = head(p.TopEvents, top)
	p.TopQueries = head(p.TopQueries, top)
	p.ByDatabase = shares(dbs, aas, pct, top)
	p.ByUser = shares(users, aas, pct, top)
	p.ByApp = shares(apps, aas, pct, top)
	p.ByBackend = shares(btypes, aas, pct, top)

	return p
}

func shares(counts map[string]int, aas, pct func(int) float64, top int) []Share {
	out := make([]Share, 0, len(counts))
	for v, c := range counts {
		out = append(out, Share{Value: v, AAS: aas(c), SharePct: pct(c)})
	}

	slices.SortFunc(out, func(a, b Share) int {
		return cmp.Or(cmp.Compare(b.AAS, a.AAS), cmp.Compare(a.Value, b.Value))
	})

	return head(out, top)
}

func head[T any](s []T, n int) []T {
	if n > 0 && len(s) > n {
		return s[:n]
	}

	return s
}
