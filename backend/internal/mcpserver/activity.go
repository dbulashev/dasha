package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/dbulashev/dasha/gen/apiclient"
)

const (
	activityMaxTop       = 20
	activityMaxTextBytes = 300
)

var errActivityUnsupported = errors.New("dasha: sample_activity needs PostgreSQL 14 or newer (501) — " +
	"older hosts have no pg_stat_activity.query_id; use wait_events and running_queries instead")

type sampleActivityArgs struct {
	Cluster           string `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance          string `json:"instance" jsonschema:"Dasha instance / host name"`
	DurationSeconds   int    `json:"duration_seconds,omitempty" jsonschema:"Sampling window, 5-60 s (default 15); the call blocks this long"`
	PeriodSeconds     int    `json:"period_seconds,omitempty" jsonschema:"Seconds between samples, 1-10 (default 1)"`
	Database          string `json:"database,omitempty" jsonschema:"Optional: count only the sessions of this database"`
	Class             string `json:"class,omitempty" jsonschema:"Optional: count only one class: CPU, IO, Lock, LWLock, BufferPin, IPC, Client, ClientIdleTx, Timeout, Extension, Activity or Other"`
	IncludeBackground bool   `json:"include_background,omitempty" jsonschema:"Also count background processes (autovacuum, walsender, checkpointer, ...) that are not idle"`
	Top               int    `json:"top,omitempty" jsonschema:"Length of every list except by_class, 1-20 (default 10)"`
}

type sampleActivityResult struct {
	*apiclient.ActivityProfile

	TextsTruncated bool `json:"texts_truncated,omitempty"`
}

func (d *DashaClient) ActivityProfile(
	ctx context.Context, params *apiclient.GetActivityProfileParams,
) (*apiclient.ActivityProfile, error) {
	r, err := d.slowAPI.GetActivityProfileWithResponse(ctx, params, d.editor(ctx))
	if err != nil {
		if isTimeout(err) {
			return nil, fmt.Errorf("%w: %w", errQueryTimeout, err)
		}

		return nil, wrapErr("sample_activity", err)
	}

	switch {
	case r.JSON200 != nil:
		return r.JSON200, nil
	case r.JSON400 != nil:
		return nil, fmt.Errorf("dasha: sample_activity: %s", r.JSON400.Message)
	case r.JSON504 != nil:
		return nil, fmt.Errorf("%w: %s", errQueryTimeout, r.JSON504.Message)
	case r.HTTPResponse != nil && r.HTTPResponse.StatusCode == http.StatusNotImplemented:
		return nil, errActivityUnsupported
	}

	return nil, statusError("sample_activity", r.HTTPResponse)
}

func sampleActivity(ctx context.Context, c *DashaClient, a sampleActivityArgs) (any, error) {
	if a.Top > activityMaxTop {
		return nil, fmt.Errorf("top must be %d or less", activityMaxTop)
	}

	params := &apiclient.GetActivityProfileParams{ //nolint:exhaustruct
		ClusterName:     a.Cluster,
		Instance:        a.Instance,
		DurationSeconds: opt(a.DurationSeconds),
		PeriodSeconds:   opt(a.PeriodSeconds),
		Top:             opt(a.Top),
		Database:        opt(a.Database),
	}

	if a.Class != "" {
		cls := apiclient.ActivityClass(a.Class)
		params.Class = &cls
	}

	if a.IncludeBackground {
		params.IncludeBackground = &a.IncludeBackground
	}

	prof, err := c.ActivityProfile(ctx, params)
	if err != nil {
		return nil, err
	}

	out := sampleActivityResult{ActivityProfile: prof}

	for i := range prof.TopQueries {
		q := &prof.TopQueries[i]
		if q.Text == nil || len(*q.Text) <= activityMaxTextBytes {
			continue
		}

		clipped := clipTo(*q.Text, activityMaxTextBytes)
		q.Text = &clipped
		out.TextsTruncated = true
	}

	return out, nil
}
