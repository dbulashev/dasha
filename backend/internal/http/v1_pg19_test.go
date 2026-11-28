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
