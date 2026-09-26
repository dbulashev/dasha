package metrics

import (
	"context"
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

type batcher struct {
	client   DatasourceClient
	maxBytes int
	maxConc  int
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

// each runs fn for every packed request, at most maxConc at a time.
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

	sem := make(chan struct{}, max(b.maxConc, 1))

	for _, r := range b.pack(items) {
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				fail(r, ctx.Err())

				return
			}

			defer func() { <-sem }()

			if err := fn(ctx, r); err != nil {
				fail(r, err)
			}
		})
	}

	wg.Wait()

	return fails
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
