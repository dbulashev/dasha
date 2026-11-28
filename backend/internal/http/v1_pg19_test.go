package http

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
	"github.com/dbulashev/dasha/internal/repository"
)

type pg19Repo struct {
	repository.Repository

	queue    *dto.MaintenanceAutovacuumQueue
	queueErr error
	limit    int
	offset   int

	recovery *dto.ReplicationRecovery
	summary  *dto.MaintenanceAutovacuumSummary
	slots    *dto.ReplicationSlots
}

func (r *pg19Repo) GetMaintenanceAutovacuumSummary(
	context.Context, string, string, string,
) (*dto.MaintenanceAutovacuumSummary, error) {
	return r.summary, nil
}

func (r *pg19Repo) GetReplicationSlots(context.Context, string, string) (*dto.ReplicationSlots, error) {
	return r.slots, nil
}

func (r *pg19Repo) GetMaintenanceAutovacuumQueue(
	_ context.Context, _, _, _ string, limit, offset int,
) (*dto.MaintenanceAutovacuumQueue, error) {
	r.limit, r.offset = limit, offset

	return r.queue, r.queueErr
}

func (r *pg19Repo) GetReplicationRecovery(context.Context, string, string) (*dto.ReplicationRecovery, error) {
	return r.recovery, nil
}

func TestGetMaintenanceAutovacuumQueue_UnsupportedVersion(t *testing.T) {
	t.Parallel()

	h := &Handlers{repo: &pg19Repo{queueErr: repository.ErrAutovacuumScoresUnsupported}} //nolint:exhaustruct

	resp, err := h.GetMaintenanceAutovacuumQueue(t.Context(), serverhttp.GetMaintenanceAutovacuumQueueRequestObject{})
	require.NoError(t, err)

	got, ok := resp.(serverhttp.GetMaintenanceAutovacuumQueue501JSONResponse)
	require.True(t, ok, "got %T", resp)
	assert.Equal(t, serverhttp.FeatureUnavailableUnsupportedVersion, got.Reason)
}

func TestGetMaintenanceAutovacuumQueue_Limit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		limit     *int
		wantLimit int
	}{
		{"default", nil, defaultAutovacuumQueueLimit},
		{"capped", shortcut.Ptr(900), maxAutovacuumQueueLimit},
		{"as given", shortcut.Ptr(20), 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &pg19Repo{queue: &dto.MaintenanceAutovacuumQueue{}}
			h := &Handlers{repo: repo} //nolint:exhaustruct

			req := serverhttp.GetMaintenanceAutovacuumQueueRequestObject{
				Params: serverhttp.GetMaintenanceAutovacuumQueueParams{Limit: tt.limit, Offset: shortcut.Ptr(40)},
			}

			_, err := h.GetMaintenanceAutovacuumQueue(t.Context(), req)
			require.NoError(t, err)
			assert.Equal(t, tt.wantLimit, repo.limit)
			assert.Equal(t, 40, repo.offset)
		})
	}
}

func TestGetMaintenanceAutovacuumQueue_Rows(t *testing.T) {
	t.Parallel()

	xid := dto.AutovacuumScoreXid
	repo := &pg19Repo{queue: &dto.MaintenanceAutovacuumQueue{
		Total: 1312,
		Rows: []dto.MaintenanceAutovacuumQueueRow{
			{Schema: "public", Table: "t", Score: 3, XidScore: 3, ForWraparound: true, Dominant: &xid},
			{Schema: "public", Table: "idle"},
		},
	}}
	h := &Handlers{repo: repo} //nolint:exhaustruct

	resp, err := h.GetMaintenanceAutovacuumQueue(t.Context(), serverhttp.GetMaintenanceAutovacuumQueueRequestObject{})
	require.NoError(t, err)

	got, ok := resp.(serverhttp.GetMaintenanceAutovacuumQueue200JSONResponse)
	require.True(t, ok, "got %T", resp)
	assert.EqualValues(t, 1312, got.Total)
	require.Len(t, got.Rows, 2)
	require.NotNil(t, got.Rows[0].Dominant)
	assert.Equal(t, "xid", *got.Rows[0].Dominant)
	assert.True(t, got.Rows[0].ForWraparound)
	assert.Nil(t, got.Rows[1].Dominant)
}

func TestGetReplicationRecovery_State(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state dto.RecoveryState
		want  serverhttp.RecoveryStatusState
	}{
		{dto.RecoveryStateOK, serverhttp.RecoveryStateOk},
		{dto.RecoveryStateNotInRecovery, serverhttp.RecoveryStateNotInRecovery},
		{dto.RecoveryStateNoPrivilege, serverhttp.RecoveryStateNoPrivilege},
		{dto.RecoveryStateUnsupportedVersion, serverhttp.RecoveryStateUnsupportedVersion},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			t.Parallel()

			h := &Handlers{repo: &pg19Repo{recovery: &dto.ReplicationRecovery{State: tt.state}}} //nolint:exhaustruct

			resp, err := h.GetReplicationRecovery(t.Context(), serverhttp.GetReplicationRecoveryRequestObject{})
			require.NoError(t, err)

			got, ok := resp.(serverhttp.GetReplicationRecovery200JSONResponse)
			require.True(t, ok, "got %T", resp)
			assert.Equal(t, tt.want, got.State)
		})
	}
}

func TestHeapTuplesWritten(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(7), heapTuplesWritten(dto.ProgressCluster{
		HeapTuplesWritten:  shortcut.Ptr[int64](7),
		HeapTuplesInserted: shortcut.Ptr[int64](100),
	}), "PG < 19 keeps its own counter")

	assert.Equal(t, int64(130), heapTuplesWritten(dto.ProgressCluster{
		HeapTuplesInserted: shortcut.Ptr[int64](100),
		HeapTuplesUpdated:  shortcut.Ptr[int64](30),
		HeapTuplesDeleted:  shortcut.Ptr[int64](5),
	}), "PG 19 falls back to inserted + updated")

	assert.Zero(t, heapTuplesWritten(dto.ProgressCluster{}))
}

func TestGetMaintenanceAutovacuumSummary_Source(t *testing.T) {
	t.Parallel()

	repo := &pg19Repo{summary: &dto.MaintenanceAutovacuumSummary{
		TablesTotal:              12,
		RunningVacuums:           3,
		RunningVacuumsWraparound: shortcut.Ptr[int32](1),
		Source:                   dto.AutovacuumSummarySourceScores,
	}}
	h := &Handlers{repo: repo} //nolint:exhaustruct

	resp, err := h.GetMaintenanceAutovacuumSummary(t.Context(), serverhttp.GetMaintenanceAutovacuumSummaryRequestObject{})
	require.NoError(t, err)

	got, ok := resp.(serverhttp.GetMaintenanceAutovacuumSummary200JSONResponse)
	require.True(t, ok, "got %T", resp)
	assert.Equal(t, serverhttp.AutovacuumSummarySourceScores, got.Source)
	require.NotNil(t, got.RunningVacuumsWraparound)
	assert.EqualValues(t, 1, *got.RunningVacuumsWraparound)
	assert.Nil(t, got.RunningVacuumsManual)
}

func TestGetReplicationSlots_OnStandby(t *testing.T) {
	t.Parallel()

	repo := &pg19Repo{slots: &dto.ReplicationSlots{
		OnStandby: shortcut.Ptr(true),
		Slots: []dto.ReplicationSlot{
			{SlotName: "a", Synced: shortcut.Ptr(true), SlotsyncSkipReason: shortcut.Ptr("wal_not_flushed")},
			{SlotName: "b"},
		},
	}}
	h := &Handlers{repo: repo} //nolint:exhaustruct

	resp, err := h.GetReplicationSlots(t.Context(), serverhttp.GetReplicationSlotsRequestObject{})
	require.NoError(t, err)

	got, ok := resp.(serverhttp.GetReplicationSlots200JSONResponse)
	require.True(t, ok, "got %T", resp)
	require.Len(t, got, 2)

	for _, slot := range got {
		require.NotNil(t, slot.OnStandby)
		assert.True(t, *slot.OnStandby)
	}

	require.NotNil(t, got[0].SlotsyncSkipReason)
	assert.Equal(t, "wal_not_flushed", *got[0].SlotsyncSkipReason)
	assert.Nil(t, got[1].Synced)
}
