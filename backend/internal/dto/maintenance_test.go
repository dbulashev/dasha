package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDominantComponent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		row  MaintenanceAutovacuumQueueRow
		want AutovacuumScoreComponent
	}{
		{"largest wins", MaintenanceAutovacuumQueueRow{VacuumScore: 0.4, AnalyzeScore: 1.7}, AutovacuumScoreAnalyze},
		{"tie goes to wraparound", MaintenanceAutovacuumQueueRow{MxidScore: 2, VacuumScore: 2}, AutovacuumScoreMxid},
		{"xid before mxid", MaintenanceAutovacuumQueueRow{XidScore: 3, MxidScore: 3}, AutovacuumScoreXid},
		{"insert before analyze", MaintenanceAutovacuumQueueRow{VacuumInsertScore: 1, AnalyzeScore: 1}, AutovacuumScoreVacuumInsert},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.row.DominantComponent()
			require.NotNil(t, got)
			assert.Equal(t, tt.want, *got)
		})
	}
}

func TestDominantComponent_AllZero(t *testing.T) {
	t.Parallel()

	assert.Nil(t, MaintenanceAutovacuumQueueRow{Score: 0}.DominantComponent())
}
