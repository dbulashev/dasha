package http

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/activity"
	"github.com/dbulashev/dasha/internal/repository"
)

func (s *Handlers) GetActivitySample(
	ctx context.Context,
	req serverhttp.GetActivitySampleRequestObject,
) (serverhttp.GetActivitySampleResponseObject, error) {
	smp, acc, err := s.activity.Get(ctx, req.Params.ClusterName, req.Params.Instance)

	switch {
	case errors.Is(err, activity.ErrUnsupportedVersion):
		return serverhttp.GetActivitySample501Response{}, nil
	case errors.Is(err, repository.ErrNotFound):
		return serverhttp.GetActivitySample404Response{}, nil
	case err != nil:
		return nil, fmt.Errorf("GetActivitySample | %w", err)
	}

	// Without pg_read_all_stats background rows come back with state and wait_event_type hidden.
	includeBackground := req.Params.IncludeBackground != nil && *req.Params.IncludeBackground && acc.CanReadAllStats

	var prev *activity.Sample
	if ts := req.Params.TextSince; ts != nil {
		if p, ok := s.activity.Recent(req.Params.ClusterName, req.Params.Instance, *ts); ok {
			prev = &p
		}
	}

	return serverhttp.GetActivitySample200JSONResponse(
		activitySampleToAPI(smp, acc, includeBackground, prev),
	), nil
}

type sentStatement struct {
	queryStart time.Time
	key        string
}

// sentTexts lists the statements whose text the client already holds: those it received in prev.
func sentTexts(prev *activity.Sample, includeBackground bool) map[int32]sentStatement {
	if prev == nil {
		return nil
	}

	out := make(map[int32]sentStatement, len(prev.Sessions))
	for _, ss := range prev.Sessions {
		if activity.Counted(ss, includeBackground) && !ss.QueryStart.IsZero() {
			out[ss.PID] = sentStatement{queryStart: ss.QueryStart, key: ss.Key}
		}
	}

	return out
}

func activitySampleToAPI(
	smp activity.Sample,
	acc activity.Access,
	includeBackground bool,
	prev *activity.Sample,
) serverhttp.ActivitySample {
	sessions := make([]serverhttp.ActivitySession, 0, len(smp.Sessions))
	sent := sentTexts(prev, includeBackground)

	for _, ss := range smp.Sessions {
		if !activity.Counted(ss, includeBackground) {
			continue
		}

		out := serverhttp.ActivitySession{
			Pid:          ss.PID,
			BackendType:  ss.BackendType,
			Database:     ss.Database,
			User:         ss.User,
			Application:  ss.Application,
			ClientAddr:   ss.ClientAddr,
			State:        ss.State,
			Class:        serverhttp.ActivityClass(activity.Class(ss)),
			WaitEvent:    ss.WaitEvent,
			QueryKey:     ss.Key,
			QueryKeyKind: serverhttp.ActivityQueryKeyKind(ss.KeyKind),
			QueryId:      strconv.FormatInt(ss.QueryID, 10),
			QueryStart:   timePtr(ss.QueryStart),
			XactStart:    timePtr(ss.XactStart),
			HoldsXmin:    ss.HoldsXmin,
		}

		// The key, not query_start, decides: each statement of a multi-statement string has its own query_id.
		if p, ok := sent[ss.PID]; ss.Text != "" && (!ok || !p.queryStart.Equal(ss.QueryStart) || p.key != ss.Key) {
			text := ss.Text
			out.Text = &text
		}

		sessions = append(sessions, out)
	}

	return serverhttp.ActivitySample{
		SampledAt:        smp.At,
		Truncated:        smp.Truncated,
		CanReadAllStats:  acc.CanReadAllStats,
		QueryIdAvailable: acc.QueryIDAvailable,
		TrackUtility:     acc.TrackUtility,
		Sessions:         sessions,
	}
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}

	return &t
}

const (
	profileDefaultDuration = 15
	profileMinDuration     = 5
	profileMaxDuration     = 60
	profileDefaultPeriod   = 1
	profileMinPeriod       = 1
	profileMaxPeriod       = 10
	profileDefaultTop      = 10
	profileMaxTop          = 50

	activityProfilePath = "/api/activity/profile"
	// How long a window may overrun duration_seconds before it ends with the samples it has.
	profileWindowSlack = 10 * time.Second
	// Must stay below proxy_read_timeout of the /api/activity/profile nginx location.
	profileWriteTimeout = profileMaxDuration*time.Second + profileWindowSlack + 5*time.Second
)

var errWindowElapsed = errors.New("activity window elapsed")

func (s *Handlers) GetActivityProfile(
	ctx context.Context,
	req serverhttp.GetActivityProfileRequestObject,
) (serverhttp.GetActivityProfileResponseObject, error) {
	p := req.Params

	duration := intOr(p.DurationSeconds, profileDefaultDuration)
	period := intOr(p.PeriodSeconds, profileDefaultPeriod)
	top := intOr(p.Top, profileDefaultTop)

	if msg := profileParamsError(duration, period, top, p.Class); msg != "" {
		return serverhttp.GetActivityProfile400JSONResponse{Message: msg}, nil
	}

	get := func(ctx context.Context) (activity.Sample, activity.Access, error) {
		return s.activity.Get(ctx, p.ClusterName, p.Instance)
	}

	col, err := collectActivity(ctx, get, duration/period, time.Duration(period)*time.Second,
		time.Duration(duration)*time.Second+profileWindowSlack)

	switch {
	case errors.Is(err, activity.ErrUnsupportedVersion):
		return serverhttp.GetActivityProfile501Response{}, nil
	case errors.Is(err, repository.ErrNotFound):
		return serverhttp.GetActivityProfile404Response{}, nil
	case err != nil:
		return nil, fmt.Errorf("GetActivityProfile | %w", err)
	case len(col.samples) == 0 && (col.lastErr == nil || repository.IsTimeout(col.lastErr)):
		return serverhttp.GetActivityProfile504JSONResponse{QueryTimeoutJSONResponse: serverhttp.QueryTimeoutJSONResponse{
			Message: fmt.Sprintf("no sample of pg_stat_activity completed in time (%d timed out)", col.failed),
		}}, nil
	case len(col.samples) == 0:
		return nil, fmt.Errorf("GetActivityProfile | all %d samples failed | %w", col.failed, col.lastErr)
	}

	filter := activity.Filter{
		Database:          deref(p.Database),
		IncludeBackground: deref(p.IncludeBackground) && col.access.CanReadAllStats,
	}

	if p.Class != nil {
		filter.Class = string(*p.Class)
	}

	prof := activity.Aggregate(col.samples, filter, top)

	return serverhttp.GetActivityProfile200JSONResponse(activityProfileToAPI(prof, col.access, col.failed)), nil
}

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}

	return *p
}

func profileParamsError(duration, period, top int, class *serverhttp.ActivityClass) string {
	switch {
	case duration < profileMinDuration || duration > profileMaxDuration:
		return fmt.Sprintf("duration_seconds must be between %d and %d", profileMinDuration, profileMaxDuration)
	case period < profileMinPeriod || period > profileMaxPeriod:
		return fmt.Sprintf("period_seconds must be between %d and %d", profileMinPeriod, profileMaxPeriod)
	case period > duration:
		return "period_seconds must not exceed duration_seconds"
	case top < 1 || top > profileMaxTop:
		return fmt.Sprintf("top must be between 1 and %d", profileMaxTop)
	case class != nil && !slices.Contains(activity.Classes, string(*class)):
		return "unknown class " + string(*class)
	}

	return ""
}

type activityCollection struct {
	samples []activity.Sample
	access  activity.Access
	failed  int
	lastErr error
}

// collectActivity takes n samples period apart. A failed sample is counted and
// skipped; a missing host, an unsupported version or a gone caller ends the window.
// Past budget the window ends early with the samples taken so far.
func collectActivity(
	ctx context.Context,
	get func(context.Context) (activity.Sample, activity.Access, error),
	n int,
	period, budget time.Duration,
) (activityCollection, error) {
	var col activityCollection

	ctx, cancel := context.WithTimeoutCause(ctx, budget, errWindowElapsed)
	defer cancel()

	stop := func() (activityCollection, error) {
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
		case errors.Is(err, activity.ErrUnsupportedVersion), errors.Is(err, repository.ErrNotFound):
			return col, err
		case ctx.Err() != nil:
			return stop()
		case err != nil:
			col.failed++
			col.lastErr = err

			continue
		}

		col.access = acc

		if k := len(col.samples); k > 0 && col.samples[k-1].At.Equal(smp.At) {
			continue
		}

		col.samples = append(col.samples, smp)
	}

	return col, nil
}

func activityProfileToAPI(p activity.Profile, acc activity.Access, failed int) serverhttp.ActivityProfile {
	out := serverhttp.ActivityProfile{
		From:             p.From,
		To:               p.To,
		Samples:          p.Samples,
		FailedSamples:    failed,
		TruncatedSamples: p.Truncated,
		CanReadAllStats:  acc.CanReadAllStats,
		QueryIdAvailable: acc.QueryIDAvailable,
		TrackUtility:     acc.TrackUtility,
		Aas:              round2(p.AAS),
		ByClass:          make([]serverhttp.ActivityClassShare, 0, len(p.ByClass)),
		TopEvents:        make([]serverhttp.ActivityEventShare, 0, len(p.TopEvents)),
		TopQueries:       make([]serverhttp.ActivityQueryShare, 0, len(p.TopQueries)),
		ByDatabase:       activitySharesToAPI(p.ByDatabase),
		ByUser:           activitySharesToAPI(p.ByUser),
		ByApplication:    activitySharesToAPI(p.ByApp),
		ByBackendType:    activitySharesToAPI(p.ByBackend),
	}

	for _, c := range p.ByClass {
		out.ByClass = append(out.ByClass, serverhttp.ActivityClassShare{
			Class: serverhttp.ActivityClass(c.Value), Aas: round2(c.AAS), SharePct: round1(c.SharePct),
		})
	}

	for _, e := range p.TopEvents {
		out.TopEvents = append(out.TopEvents, serverhttp.ActivityEventShare{
			Class: serverhttp.ActivityClass(e.Class), WaitEvent: e.WaitEvent, Aas: round2(e.AAS), SharePct: round1(e.SharePct),
		})
	}

	for _, q := range p.TopQueries {
		row := serverhttp.ActivityQueryShare{
			QueryKey:     q.Key,
			QueryKeyKind: serverhttp.ActivityQueryKeyKind(q.KeyKind),
			QueryId:      strconv.FormatInt(q.QueryID, 10),
			Aas:          round2(q.AAS),
			SharePct:     round1(q.SharePct),
			ByClass:      roundShares(q.ByClass),
			DistinctPids: q.DistinctPIDs,
		}

		if q.Text != "" {
			text := q.Text
			row.Text = &text
		}

		out.TopQueries = append(out.TopQueries, row)
	}

	return out
}

func activitySharesToAPI(in []activity.Share) []serverhttp.ActivityShare {
	out := make([]serverhttp.ActivityShare, 0, len(in))
	for _, s := range in {
		out = append(out, serverhttp.ActivityShare{Value: s.Value, Aas: round2(s.AAS), SharePct: round1(s.SharePct)})
	}

	return out
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func roundShares(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = round1(v)
	}

	return out
}
