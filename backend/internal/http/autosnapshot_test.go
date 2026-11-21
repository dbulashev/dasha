package http

import (
	"context"
	"errors"
	"testing"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/autosnapshot"
)

// errConfigStore fails every write with err; reads are not exercised.
type errConfigStore struct {
	autosnapshot.ConfigStore
	err error
}

func (f errConfigStore) UpdateConfig(
	context.Context, string, func(autosnapshot.Config, map[string]map[string]any) (autosnapshot.Config, error),
) error {
	return f.err
}

func (f errConfigStore) UpdateClusterOverride(
	context.Context, string, string, func(autosnapshot.Config) (map[string]any, error),
) error {
	return f.err
}

func configHandlers(err error) *Handlers {
	return &Handlers{autosnapshotCfg: autosnapshot.NewConfigService(errConfigStore{err: err}, nil)} //nolint:exhaustruct
}

func validConfigBody() *serverhttp.AutoSnapshotConfig {
	return &serverhttp.AutoSnapshotConfig{ //nolint:exhaustruct
		PollInterval:         "30s",
		MaxSnapshotFrequency: "1h",
		LockProbeCount:       5,
		LockProbeInterval:    "500ms",
		HotSchedule:          "0 3 * * *",
		HotTopN:              100,
		HotRetentionDays:     180,
		IOSchedule:           "*/5 * * * *",
		IORetentionDays:      30,
		Defaults: serverhttp.AutoSnapshotTriggerDefaults{
			ActivitySpike: serverhttp.ActivitySpikeTrigger{ //nolint:exhaustruct
				WindowSize:         "30m",
				ActiveThresholdPct: 50,
				SpikeDuration:      "2m",
			},
			RoleChange: serverhttp.RoleChangeTrigger{Direction: serverhttp.Both}, //nolint:exhaustruct
		},
	}
}

func TestPutAutosnapshotConfigMapping(t *testing.T) {
	t.Parallel()

	invalid := errors.Join(autosnapshot.ErrInvalidConfig, errors.New("x"))

	put := func(h *Handlers) (serverhttp.PutAutosnapshotConfigResponseObject, error) {
		return h.PutAutosnapshotConfig(t.Context(), serverhttp.PutAutosnapshotConfigRequestObject{Body: validConfigBody()})
	}

	if resp, _ := put(&Handlers{}); !isType[serverhttp.PutAutosnapshotConfig501Response](resp) { //nolint:exhaustruct
		t.Fatalf("no service: got %T", resp)
	}

	if resp, err := put(configHandlers(invalid)); err != nil || !isType[serverhttp.PutAutosnapshotConfig400Response](resp) {
		t.Fatalf("invalid: got %T, %v", resp, err)
	}

	if resp, err := put(configHandlers(errors.New("db down"))); err == nil {
		t.Fatalf("db error: got %T, want error", resp)
	}

	if resp, err := put(configHandlers(nil)); err != nil || !isType[serverhttp.PutAutosnapshotConfig204Response](resp) {
		t.Fatalf("ok: got %T, %v", resp, err)
	}
}

func TestPutAutosnapshotConfigBadCronIs400(t *testing.T) {
	t.Parallel()

	h := &Handlers{autosnapshotCfg: autosnapshot.NewConfigService(syncConfigStore{}, nil)} //nolint:exhaustruct

	resp, err := h.PutAutosnapshotConfig(t.Context(), serverhttp.PutAutosnapshotConfigRequestObject{Body: validConfigBody()})
	if err != nil || !isType[serverhttp.PutAutosnapshotConfig204Response](resp) {
		t.Fatalf("valid body: got %T, %v", resp, err)
	}

	body := validConfigBody()
	body.HotSchedule = "daily"

	resp, err = h.PutAutosnapshotConfig(t.Context(), serverhttp.PutAutosnapshotConfigRequestObject{Body: body})
	if err != nil || !isType[serverhttp.PutAutosnapshotConfig400Response](resp) {
		t.Fatalf("bad cron: got %T, %v", resp, err)
	}
}

func TestPutAutosnapshotClusterMapping(t *testing.T) {
	t.Parallel()

	req := serverhttp.PutAutosnapshotClusterRequestObject{
		Name: "c1",
		Body: &serverhttp.AutoSnapshotClusterOverrideInput{Overrides: map[string]any{}},
	}

	invalid := errors.Join(autosnapshot.ErrInvalidConfig, errors.New("x"))

	if resp, _ := (&Handlers{}).PutAutosnapshotCluster(t.Context(), req); !isType[serverhttp.PutAutosnapshotCluster501Response](resp) { //nolint:exhaustruct
		t.Fatalf("no service: got %T", resp)
	}

	if resp, err := configHandlers(invalid).PutAutosnapshotCluster(t.Context(), req); err != nil ||
		!isType[serverhttp.PutAutosnapshotCluster400Response](resp) {
		t.Fatalf("invalid: got %T, %v", resp, err)
	}

	if resp, err := configHandlers(errors.New("db down")).PutAutosnapshotCluster(t.Context(), req); err == nil {
		t.Fatalf("db error: got %T, want error", resp)
	}
}

// syncConfigStore runs the callback against a zero current config, so the
// service's own validation decides the outcome.
type syncConfigStore struct{ autosnapshot.ConfigStore }

func (syncConfigStore) UpdateConfig(
	_ context.Context, _ string, fn func(autosnapshot.Config, map[string]map[string]any) (autosnapshot.Config, error),
) error {
	_, err := fn(autosnapshot.Config{HotColdWindowDays: 7}, nil) //nolint:exhaustruct

	return err
}

func isType[T any](v any) bool {
	_, ok := v.(T)

	return ok
}
