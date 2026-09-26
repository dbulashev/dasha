package healthscore

import (
	"context"
	"sync"
	"time"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/metrics"
)

// inputsHold is how long one read serves the card and the recommendations,
// which the UI requests together.
const inputsHold = 5 * time.Second

type inputsKey struct {
	target   metrics.TargetRef
	database string
}

// inputs is everything one score reads from outside. metrics is nil when the
// datasource is off or not consulted (database drill-down).
type inputs struct {
	metrics  *metrics.RawResult
	snap     *dto.HealthScoreMetrics
	snapErr  error
	seqWorst float64
	seqKnown bool
	seqErr   error
}

type inputsFlight struct {
	done chan struct{}
	in   inputs
}

// read runs the datasource, the catalog snapshot and the sequence headroom
// concurrently. The headroom is read on standbys too, before in_recovery is
// known; the repository caches it per database.
func (s *Scorer) read(ctx context.Context, t metrics.TargetRef, database string, pre *metrics.RawResult) inputs {
	in := inputs{metrics: pre}

	var wg sync.WaitGroup

	if pre == nil && database == "" && s.metrics != nil && s.metrics.Enabled() {
		wg.Go(func() {
			raw, sig, err := s.metrics.CurrentRaw(ctx, t.Cluster, t.Instance)
			in.metrics = &metrics.RawResult{Raw: raw, Signals: sig, Err: err}
		})
	}

	wg.Go(func() {
		in.snap, in.snapErr = s.repo.GetHealthScoreMetrics(ctx, t.Cluster, t.Instance, database)
	})

	wg.Go(func() {
		in.seqWorst, in.seqKnown, in.seqErr = s.repo.GetSequenceHeadroom(ctx, t.Cluster, t.Instance, database)
	})

	wg.Wait()

	return in
}

// coalescedInputs shares one read per key between concurrent callers and for
// inputsHold after it completes. The read is detached from the first caller's
// cancellation; a caller that gives up gets its own context error.
func (s *Scorer) coalescedInputs(ctx context.Context, t metrics.TargetRef, database string) inputs {
	key := inputsKey{target: t, database: database}

	s.mu.Lock()

	f, ok := s.flights[key]
	if !ok {
		f = &inputsFlight{done: make(chan struct{})}
		s.flights[key] = f

		go func() {
			f.in = s.read(context.WithoutCancel(ctx), t, database, nil)
			close(f.done)

			time.AfterFunc(inputsHold, func() {
				s.mu.Lock()
				defer s.mu.Unlock()

				if s.flights[key] == f {
					delete(s.flights, key)
				}
			})
		}()
	}

	s.mu.Unlock()

	select {
	case <-f.done:
		return f.in
	case <-ctx.Done():
		return inputs{snapErr: ctx.Err(), seqErr: ctx.Err()}
	}
}
