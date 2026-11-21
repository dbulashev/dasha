package activity

import (
	"context"
	"errors"
	"time"

	"github.com/dbulashev/dasha/internal/pkg/errs"
)

var errWindowElapsed = errors.New("activity window elapsed")

type Collection struct {
	Samples []Sample
	Access  Access
	Failed  int
	LastErr error
}

// Collect takes n samples period apart. A failed sample is counted and skipped;
// a missing host, an unsupported version or a gone caller ends the window.
// Past budget the window ends early with the samples taken so far, which is not an error.
func (s *Sampler) Collect(
	ctx context.Context,
	cluster, instance string,
	n int,
	period, budget time.Duration,
) (Collection, error) {
	get := func(ctx context.Context) (Sample, Access, error) {
		return s.Get(ctx, cluster, instance)
	}

	return collect(ctx, get, n, period, budget)
}

func collect(
	ctx context.Context,
	get func(context.Context) (Sample, Access, error),
	n int,
	period, budget time.Duration,
) (Collection, error) {
	var col Collection

	ctx, cancel := context.WithTimeoutCause(ctx, budget, errWindowElapsed)
	defer cancel()

	stop := func() (Collection, error) {
		if errors.Is(context.Cause(ctx), errWindowElapsed) {
			return col, nil
		}

		return col, ctx.Err()
	}

	ticker := time.NewTicker(period)
	defer ticker.Stop()

	for i := range n {
		if i > 0 {
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return stop()
			}
		}

		smp, acc, err := get(ctx)

		switch {
		case errors.Is(err, ErrUnsupportedVersion), errors.Is(err, errs.ErrNotFound):
			return col, err
		case ctx.Err() != nil:
			return stop()
		case err != nil:
			col.Failed++
			col.LastErr = err

			continue
		}

		col.Access = acc

		if k := len(col.Samples); k > 0 && col.Samples[k-1].At.Equal(smp.At) {
			continue
		}

		col.Samples = append(col.Samples, smp)
	}

	return col, nil
}
