//go:build integration

package storage

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/autosnapshot"
	"github.com/dbulashev/dasha/internal/testinfra"
)

func newAutosnapshotConfigTestStorage(t *testing.T) *Storage {
	t.Helper()

	pool := testinfra.IsolateEmptyPool(t)

	for _, ddl := range []string{
		createAutosnapshotConfigGlobalSQL, seedAutosnapshotConfigGlobalSQL, createAutosnapshotConfigClusterSQL,
		addAutosnapshotLockConfigSQL, addAutosnapshotResetConfigSQL, addAutosnapshotHotConfigSQL,
		addAutosnapshotHotColdWindowSQL, addAutosnapshotIOConfigSQL,
	} {
		_, err := pool.Exec(t.Context(), ddl)
		require.NoError(t, err, "config DDL")
	}

	return &Storage{pool: pool, ddlPool: pool, logger: zap.NewNop()}
}

// mustNotRun fails if ch closes within the window: the other transaction got
// through while it should have been waiting on the config row.
func mustNotRun(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-ch:
		t.Fatalf("%s ran while the config row was locked", what)
	case <-time.After(300 * time.Millisecond):
	}
}

// awaitEntered waits for the worker's callback to start; a worker error
// (Begin, config read) is reported instead of hanging on entered.
func awaitEntered(t *testing.T, entered <-chan struct{}, workerErr <-chan error) {
	t.Helper()

	select {
	case <-entered:
	case err := <-workerErr:
		t.Fatalf("worker finished before entering the callback: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("callback was not entered in time")
	}
}

func TestUpdateConfigBlocksOverrideWrite(t *testing.T) {
	s := newAutosnapshotConfigTestStorage(t)
	ctx := t.Context()

	entered, release := make(chan struct{}), make(chan struct{})
	cfgErr := make(chan error, 1)

	go func() {
		cfgErr <- s.UpdateConfig(ctx, "a", func(cur autosnapshot.Config, _ map[string]map[string]any) (autosnapshot.Config, error) {
			close(entered)
			<-release

			cur.Defaults.ActivitySpike.WindowSize = 10 * time.Minute

			return cur, nil
		})
	}()

	awaitEntered(t, entered, cfgErr)

	var seen autosnapshot.Config

	ovStarted := make(chan struct{})
	ovErr := make(chan error, 1)

	go func() {
		ovErr <- s.UpdateClusterOverride(ctx, "c1", "b", func(cfg autosnapshot.Config) (map[string]any, error) {
			close(ovStarted)

			seen = cfg

			return map[string]any{"activity_spike": map[string]any{"spike_duration": "4m"}}, nil
		})
	}()

	mustNotRun(t, ovStarted, "override callback")
	close(release)

	require.NoError(t, <-cfgErr)
	require.NoError(t, <-ovErr)
	assert.Equal(t, 10*time.Minute, seen.Defaults.ActivitySpike.WindowSize, "override must be checked against the new config")

	ovs, err := s.ListClusterOverrides(ctx)
	require.NoError(t, err)
	assert.Contains(t, ovs, "c1")
}

func TestOverrideWriteBlocksUpdateConfig(t *testing.T) {
	s := newAutosnapshotConfigTestStorage(t)
	ctx := t.Context()

	entered, release := make(chan struct{}), make(chan struct{})
	ovErr := make(chan error, 1)

	go func() {
		ovErr <- s.UpdateClusterOverride(ctx, "c1", "b", func(autosnapshot.Config) (map[string]any, error) {
			close(entered)
			<-release

			return map[string]any{"activity_spike": map[string]any{"spike_duration": "4m"}}, nil
		})
	}()

	awaitEntered(t, entered, ovErr)

	var seen map[string]map[string]any

	cfgStarted := make(chan struct{})
	cfgErr := make(chan error, 1)

	go func() {
		cfgErr <- s.UpdateConfig(ctx, "a", func(cur autosnapshot.Config, ovs map[string]map[string]any) (autosnapshot.Config, error) {
			close(cfgStarted)

			seen = ovs

			return cur, nil
		})
	}()

	mustNotRun(t, cfgStarted, "config callback")
	close(release)

	require.NoError(t, <-ovErr)
	require.NoError(t, <-cfgErr)
	assert.Contains(t, seen, "c1", "config must be checked against the committed override")
}

func TestUpdateConfigCallbackErrorRollsBack(t *testing.T) {
	s := newAutosnapshotConfigTestStorage(t)
	ctx := t.Context()

	before, err := s.GetAutosnapshotConfig(ctx)
	require.NoError(t, err)

	errReject := errors.New("reject")

	err = s.UpdateConfig(ctx, "a", func(cur autosnapshot.Config, _ map[string]map[string]any) (autosnapshot.Config, error) {
		return cur, errReject
	})
	require.ErrorIs(t, err, errReject)

	after, err := s.GetAutosnapshotConfig(ctx)
	require.NoError(t, err)
	assert.Equal(t, before.UpdatedAt, after.UpdatedAt)
	assert.Nil(t, after.UpdatedBy)
}

func TestUpdateConfigRoundTrip(t *testing.T) {
	s := newAutosnapshotConfigTestStorage(t)
	ctx := t.Context()

	require.NoError(t, s.UpdateConfig(ctx, "admin", func(cur autosnapshot.Config, _ map[string]map[string]any) (autosnapshot.Config, error) {
		cur.HotColdWindowDays = 21
		cur.HotSchedule = "0 4 * * *"

		return cur, nil
	}))

	got, err := s.GetAutosnapshotConfig(ctx)
	require.NoError(t, err)
	assert.Equal(t, 21, got.HotColdWindowDays)
	assert.Equal(t, "0 4 * * *", got.HotSchedule)
	require.NotNil(t, got.UpdatedBy)
	assert.Equal(t, "admin", *got.UpdatedBy)
}

func TestUpdateClusterOverrideEmptyDeletes(t *testing.T) {
	s := newAutosnapshotConfigTestStorage(t)
	ctx := t.Context()

	set := func(ov map[string]any) {
		require.NoError(t, s.UpdateClusterOverride(ctx, "c1", "b", func(autosnapshot.Config) (map[string]any, error) {
			return ov, nil
		}))
	}

	set(map[string]any{"role_change": map[string]any{"enabled": false}})
	assert.Equal(t, 1, countRows(t, s, "autosnapshot_config_cluster"))

	set(map[string]any{})
	assert.Equal(t, 0, countRows(t, s, "autosnapshot_config_cluster"))
}
