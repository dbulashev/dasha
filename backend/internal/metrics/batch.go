package metrics

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TargetRef names a Dasha instance.
type TargetRef struct{ Cluster, Instance string }

type batchKey struct {
	Target TargetRef
	Signal SignalKind
}

// batchItem is one catalog expression shared by every key that renders to it.
type batchItem struct {
	Expr string
	Keys []batchKey
}

type batchFailure struct {
	Keys []batchKey
	Err  error
}

// batchLabel tags each glued term; catalog expressions must not produce it.
const batchLabel = "dasha_q"

// batcher's sem bounds concurrent requests across all its calls.
type batcher struct {
	client   DatasourceClient
	maxBytes int
	sem      chan struct{}
}

func newBatcher(client DatasourceClient, maxBytes, maxConc int) *batcher {
	return &batcher{client: client, maxBytes: maxBytes, sem: make(chan struct{}, max(maxConc, 1))}
}

type batchReq struct {
	expr  string
	items []batchItem
	glued bool
}

func glueTerm(expr string, i int) string {
	return `label_replace(` + expr + `, "` + batchLabel + `", "` + strconv.Itoa(i) + `", "", "")`
}

// pack joins items into as few `or`-glued requests as fit maxBytes each; an
// item that does not fit alone goes out bare.
func (b *batcher) pack(items []batchItem) []batchReq {
	var (
		out []batchReq
		cur []batchItem
		sb  strings.Builder
	)

	flush := func() {
		if len(cur) > 0 {
			out = append(out, batchReq{expr: sb.String(), items: cur, glued: true})
		}

		cur = nil

		sb.Reset()
	}

	for _, it := range items {
		if len(cur) > 0 {
			term := " or " + glueTerm(it.Expr, len(cur))
			if sb.Len()+len(term) <= b.maxBytes {
				sb.WriteString(term)

				cur = append(cur, it)

				continue
			}

			flush()
		}

		term := glueTerm(it.Expr, 0)
		if len(term) > b.maxBytes {
			out = append(out, batchReq{expr: it.Expr, items: []batchItem{it}})

			continue
		}

		sb.WriteString(term)

		cur = append(cur, it)
	}

	flush()

	return out
}

func (r batchReq) item(labels map[string]string) (batchItem, bool) {
	if !r.glued {
		return r.items[0], true
	}

	i, err := strconv.Atoi(labels[batchLabel])
	if err != nil || i < 0 || i >= len(r.items) {
		return batchItem{}, false
	}

	return r.items[i], true
}

// each runs fn for every packed request, each holding a slot of b.sem.
func (b *batcher) each(ctx context.Context, items []batchItem, fn func(context.Context, batchReq) error) []batchFailure {
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		fails []batchFailure
	)

	fail := func(r batchReq, err error) {
		f := batchFailure{Err: err}
		for _, it := range r.items {
			f.Keys = append(f.Keys, it.Keys...)
		}

		mu.Lock()
		fails = append(fails, f)
		mu.Unlock()
	}

	for _, r := range b.pack(items) {
		wg.Go(func() {
			select {
			case b.sem <- struct{}{}:
			case <-ctx.Done():
				fail(r, ctx.Err())

				return
			}

			defer func() { <-b.sem }()

			b.run(ctx, r, fn, fail)
		})
	}

	wg.Wait()

	return fails
}

func (b *batcher) run(ctx context.Context, r batchReq, fn func(context.Context, batchReq) error, fail func(batchReq, error)) {
	if err := fn(ctx, r); err != nil {
		b.split(ctx, r, err, fn, fail)
	}
}

// split retries a rejected request spanning several targets as two halves, down
// to the offending items. Both halves rejected fails r whole.
func (b *batcher) split(ctx context.Context, r batchReq, err error, fn func(context.Context, batchReq) error, fail func(batchReq, error)) {
	if !rejected(err) || ctx.Err() != nil || !multiTarget(r.items) {
		fail(r, err)

		return
	}

	mid := len(r.items) / 2

	var halves []batchReq

	halves = append(halves, b.pack(r.items[:mid])...)
	halves = append(halves, b.pack(r.items[mid:])...)

	errs := make([]error, len(halves))
	failed := 0

	for i, h := range halves {
		if errs[i] = fn(ctx, h); errs[i] != nil {
			failed++
		}
	}

	if failed == len(halves) {
		fail(r, errs[0])

		return
	}

	for i, h := range halves {
		if errs[i] != nil {
			b.split(ctx, h, errs[i], fn, fail)
		}
	}
}

func multiTarget(items []batchItem) bool {
	var (
		first TargetRef
		seen  bool
	)

	for _, it := range items {
		for _, k := range it.Keys {
			if !seen {
				first, seen = k.Target, true
			} else if k.Target != first {
				return true
			}
		}
	}

	return false
}

func rejected(err error) bool {
	var se *StatusError

	return errors.As(err, &se) && (se.Code == http.StatusBadRequest || se.Code == http.StatusUnprocessableEntity)
}

func (b *batcher) instant(ctx context.Context, items []batchItem, at time.Time) (map[batchKey]float64, []batchFailure) {
	var mu sync.Mutex

	out := make(map[batchKey]float64)

	fails := b.each(ctx, items, func(ctx context.Context, r batchReq) error {
		samples, err := b.client.QueryInstant(ctx, r.expr, at)
		if err != nil {
			return err
		}

		mu.Lock()
		defer mu.Unlock()

		for _, s := range samples {
			it, ok := r.item(s.Labels)
			if !ok {
				continue
			}

			for _, k := range it.Keys {
				if _, seen := out[k]; !seen {
					out[k] = s.Value
				}
			}
		}

		return nil
	})

	return out, fails
}

func (b *batcher) rangeQ(ctx context.Context, items []batchItem, rg Range) (map[batchKey][]SeriesPoint, []batchFailure) {
	var mu sync.Mutex

	out := make(map[batchKey][]SeriesPoint)

	fails := b.each(ctx, items, func(ctx context.Context, r batchReq) error {
		series, err := b.client.QueryRange(ctx, r.expr, rg)
		if err != nil {
			return err
		}

		mu.Lock()
		defer mu.Unlock()

		for _, s := range series {
			it, ok := r.item(s.Labels)
			if !ok {
				continue
			}

			for _, k := range it.Keys {
				if _, seen := out[k]; !seen {
					out[k] = s.Points
				}
			}
		}

		return nil
	})

	return out, fails
}
