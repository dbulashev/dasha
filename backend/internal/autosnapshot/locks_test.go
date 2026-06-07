package autosnapshot

import (
	"context"
	"errors"
	"testing"

	"github.com/dbulashev/dasha/internal/dto"
)

// fakeProber replays a scripted sequence of probe results / errors.
type fakeProber struct {
	results [][]dto.QueryBlocked
	errs    []error
	calls   int
}

func (f *fakeProber) GetQueriesBlocked(_ context.Context, _, _, _ string) ([]dto.QueryBlocked, error) {
	i := f.calls
	f.calls++

	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}

	if i < len(f.results) {
		return f.results[i], nil
	}

	return nil, nil
}

func blockedRows(pids ...int32) []dto.QueryBlocked {
	out := make([]dto.QueryBlocked, 0, len(pids))
	for _, p := range pids {
		out = append(out, dto.QueryBlocked{BlockedPid: p}) //nolint:exhaustruct
	}

	return out
}

func withWait(pid int32, ms float64) dto.QueryBlocked {
	return dto.QueryBlocked{BlockedPid: pid, BlockedDurationMs: &ms} //nolint:exhaustruct
}

func TestCaptureLocks_HarshestByCount(t *testing.T) {
	t.Parallel()

	repo := &fakeProber{results: [][]dto.QueryBlocked{
		blockedRows(1, 2),
		blockedRows(1, 2, 3, 4, 5),
		blockedRows(1, 2, 3),
	}}

	c := CaptureLocks(context.Background(), repo, "c", "i", "db", 3, 0)
	if !c.Captured {
		t.Fatalf("expected Captured")
	}

	if c.BlockedCount != 5 {
		t.Errorf("BlockedCount = %d, want 5 (harshest probe)", c.BlockedCount)
	}
}

func TestCaptureLocks_TieBreakByWait(t *testing.T) {
	t.Parallel()

	repo := &fakeProber{results: [][]dto.QueryBlocked{
		{withWait(1, 100), withWait(2, 50)},
		{withWait(1, 500), withWait(2, 50)}, // same count (2), longer max wait
	}}

	c := CaptureLocks(context.Background(), repo, "c", "i", "db", 2, 0)
	if c.BlockedCount != 2 || c.MaxWaitMs != 500 {
		t.Errorf("got count=%d wait=%v, want count=2 wait=500", c.BlockedCount, c.MaxWaitMs)
	}
}

func TestCaptureLocks_CapsAndSortsRows(t *testing.T) {
	t.Parallel()

	rows := make([]dto.QueryBlocked, 0, 150)
	for i := int32(1); i <= 150; i++ {
		rows = append(rows, withWait(i, float64(i))) // wait grows with pid
	}

	repo := &fakeProber{results: [][]dto.QueryBlocked{rows}}

	c := CaptureLocks(context.Background(), repo, "c", "i", "db", 1, 0)
	if c.BlockedCount != 150 {
		t.Errorf("BlockedCount = %d, want 150", c.BlockedCount)
	}

	if len(c.Rows) != maxLockRows {
		t.Fatalf("len(Rows) = %d, want %d", len(c.Rows), maxLockRows)
	}

	// Sorted by wait desc → the top row is the longest-waiting (pid 150).
	if c.Rows[0].BlockedPid != 150 {
		t.Errorf("top row pid = %d, want 150 (highest wait first)", c.Rows[0].BlockedPid)
	}
}

func TestCaptureLocks_BestEffortOnError(t *testing.T) {
	t.Parallel()

	repo := &fakeProber{errs: []error{errors.New("boom"), errors.New("boom"), errors.New("boom")}}

	c := CaptureLocks(context.Background(), repo, "c", "i", "db", 3, 0)
	if c.Captured {
		t.Errorf("expected Captured=false when all probes error")
	}

	if c.Error == "" {
		t.Errorf("expected Error to be set")
	}
}
