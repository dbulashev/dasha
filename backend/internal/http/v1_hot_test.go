package http

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/hotobjects"
)

// hotStore scripts the stored side; anything further panics on the nil
// embedded interface.
type hotStore struct {
	hotobjects.ReadStore
	snap *hotobjects.Snapshot
	top  []hotobjects.TopEntry
}

func (f hotStore) GetHotSnapshot(context.Context, string, string, *time.Time) (*hotobjects.Snapshot, error) {
	return f.snap, nil
}

func (hotStore) GetHotSnapshotBefore(context.Context, string, string, time.Time) (*hotobjects.Snapshot, error) {
	return nil, nil //nolint:nilnil
}

func (f hotStore) GetHotTop(
	context.Context, uuid.UUID, time.Time, hotobjects.Kind, hotobjects.Class, int, int,
) ([]hotobjects.TopEntry, error) {
	return f.top, nil
}

func (hotStore) ListHotSnapshotDates(context.Context, string, string, int) ([]time.Time, error) {
	return nil, nil
}

func (hotStore) GetHotAnchorsForObject(
	context.Context, string, string, hotobjects.Kind, string, string,
) ([]hotobjects.AnchorRow, error) {
	return nil, nil
}

func hotHandlers(st hotStore) *Handlers {
	return &Handlers{hot: hotobjects.NewReader(st, nil, zap.NewNop())} //nolint:exhaustruct
}

func tablesHotReq() serverhttp.GetTablesHotRequestObject {
	return serverhttp.GetTablesHotRequestObject{
		Params: serverhttp.GetTablesHotParams{ClusterName: "c1", Database: "app"}, //nolint:exhaustruct
	}
}

func percentileReq() serverhttp.GetHotPercentileRequestObject {
	return serverhttp.GetHotPercentileRequestObject{
		Params: serverhttp.GetHotPercentileParams{ //nolint:exhaustruct
			ClusterName: "c1", Database: "app", Kind: serverhttp.Table, Schema: "public", Object: "orders",
		},
	}
}

func TestHotWithoutStorage(t *testing.T) {
	t.Parallel()

	h := &Handlers{} //nolint:exhaustruct

	resp, err := h.GetTablesHot(context.Background(), tablesHotReq())
	require.NoError(t, err)
	assert.IsType(t, serverhttp.GetTablesHot501Response{}, resp)

	pResp, err := h.GetHotPercentile(context.Background(), percentileReq())
	require.NoError(t, err)
	assert.IsType(t, serverhttp.GetHotPercentile501Response{}, pResp)
}

func TestHotNotFound(t *testing.T) {
	t.Parallel()

	resp, err := hotHandlers(hotStore{}).GetTablesHot(context.Background(), tablesHotReq()) //nolint:exhaustruct
	require.NoError(t, err)
	assert.IsType(t, serverhttp.GetTablesHot404Response{}, resp, "no snapshot")

	st := hotStore{snap: &hotobjects.Snapshot{ID: uuid.New()}} //nolint:exhaustruct

	pResp, err := hotHandlers(st).GetHotPercentile(context.Background(), percentileReq())
	require.NoError(t, err)
	assert.IsType(t, serverhttp.GetHotPercentile404Response{}, pResp, "no anchors")
}

func TestTablesHotMapping(t *testing.T) {
	t.Parallel()

	st := hotStore{
		snap: &hotobjects.Snapshot{ //nolint:exhaustruct
			ID:       uuid.New(),
			Coverage: map[hotobjects.Class]hotobjects.KindRatio{hotobjects.ClassReads: {Tables: 0.8, Indexes: 0.1}},
		},
		top: []hotobjects.TopEntry{{ //nolint:exhaustruct
			Rank: 1, Schema: "public", Object: "orders",
			PerHost: map[string]hotobjects.HostDelta{
				"h2": {}, //nolint:exhaustruct
				"h1": {}, //nolint:exhaustruct
				"h3": {}, //nolint:exhaustruct
			},
		}},
	}

	resp, err := hotHandlers(st).GetTablesHot(context.Background(), tablesHotReq())
	require.NoError(t, err)

	report, ok := resp.(serverhttp.GetTablesHot200JSONResponse)
	require.True(t, ok)

	assert.InDelta(t, 0.8, report.Snapshot.Coverage, 1e-9)
	assert.NotNil(t, report.Snapshot.Dates, "dates stay a JSON array")
	assert.Nil(t, report.Snapshot.Histogram)

	require.Len(t, report.Entries, 1)

	hosts := make([]string, 0, 3)
	for _, h := range report.Entries[0].PerHost {
		hosts = append(hosts, h.Instance)
	}

	assert.Equal(t, []string{"h1", "h2", "h3"}, hosts)
}
