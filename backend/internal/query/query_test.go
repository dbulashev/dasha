package query

import (
	"testing"

	"github.com/dbulashev/dasha/internal/enums"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGet_ValidQuery_ReturnsSQL(t *testing.T) {
	t.Parallel()

	sql, err := Get(170000, enums.QueryCommonInRecovery, nil)
	require.NoError(t, err)
	assert.Contains(t, sql, "pg_is_in_recovery")
}

func TestGet_InvalidQuery_ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := Get(170000, enums.Query("nonexistent/query"), nil)
	assert.ErrorIs(t, err, enums.ErrInvalidQuery)
}

func TestGet_QueryWithNoVersionDirs_ReturnsBaseTemplate(t *testing.T) {
	t.Parallel()

	// common/in_recovery has no version-specific overrides
	sql, err := Get(90000, enums.QueryCommonInRecovery, nil)
	require.NoError(t, err)
	assert.Contains(t, sql, "pg_is_in_recovery")

	sql2, err := Get(170000, enums.QueryCommonInRecovery, nil)
	require.NoError(t, err)
	assert.Equal(t, sql, sql2, "should return same base template for any version")
}

func TestGet_TemplateDataExecution(t *testing.T) {
	t.Parallel()

	// indexes/bloat has no Go template variables — just verify SQL is returned
	sql, err := Get(170000, enums.QueryIndexesBloat, nil)
	require.NoError(t, err)
	assert.Contains(t, sql, "wastedbytes")
	assert.NotContains(t, sql, "{{", "template should be fully rendered")
}

// TestFindTemplate_VersionSelection tests the version-specific template selection logic.
//
// Algorithm: keeps dirs where dirVersion > serverVersion, picks MIN of those.
// If no dirs qualify, falls back to base template.
//
// Convention: dir N contains the legacy template for server versions < N.
// Base template is for the newest supported version.
//
// Example for top_10_by_time (dirs: 150000, 170000):
// - PG14 (140000): MIN dir > 140000 = 150000 (blk_read_time only)
// - PG15 (150000): MIN dir > 150000 = 170000 (blk_read_time + temp_blk_read_time)
// - PG17 (170000): no dir > 170000 → base (shared_blk_read_time + temp_blk_read_time)
func TestFindTemplate_VersionSelection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		queryName     string
		serverVersion int
		wantContains  string // unique substring to identify which template was selected
	}{
		// common/in_recovery — no version dirs, always base
		{
			name:          "in_recovery_any_version",
			queryName:     "common/in_recovery",
			serverVersion: 140000,
			wantContains:  "pg_is_in_recovery",
		},

		// queries/running — dirs: 90600, 100000
		// Base template has `backend_type` column directly
		// 100000/ has `NULL::text AS backend_type`
		// 90600/ has the oldest variant
		{
			name:          "running_pg17_uses_base",
			queryName:     "queries/running",
			serverVersion: 170000,
			wantContains:  "backend_type\n", // base has raw backend_type column
		},
		{
			name:          "running_pg14_uses_base",
			queryName:     "queries/running",
			serverVersion: 140000,
			wantContains:  "backend_type\n", // PG14 > all dirs → base
		},
		{
			name:          "running_pg96_uses_100000_dir",
			queryName:     "queries/running",
			serverVersion: 90600,
			wantContains:  "NULL::text", // MIN dir > 90600 = 100000
		},

		// queries/top_10_by_time — dirs: 150000, 170000
		// 150000/ has blk_read_time only (no temp) — for PG14
		// 170000/ has blk_read_time + temp_blk_read_time — for PG15-16
		// Base has shared_blk_read_time + temp_blk_read_time — for PG17+
		{
			name:          "top10_pg14_uses_150000_dir",
			queryName:     "queries/top_10_by_time",
			serverVersion: 140000,
			wantContains:  "blk_read_time", // MIN dir > 140000 = 150000 (no temp_blk)
		},
		{
			name:          "top10_pg15_uses_170000_dir",
			queryName:     "queries/top_10_by_time",
			serverVersion: 150000,
			wantContains:  "temp_blk_read_time", // MIN dir > 150000 = 170000
		},
		{
			name:          "top10_pg17_uses_base",
			queryName:     "queries/top_10_by_time",
			serverVersion: 170000,
			wantContains:  "shared_blk_read_time", // no dir > 170000 → base
		},
		{
			name:          "top10_pg18_uses_base",
			queryName:     "queries/top_10_by_time",
			serverVersion: 180000,
			wantContains:  "shared_blk_read_time", // no dir > 180000 → base
		},

		// progress/vacuum — dir: 170000
		{
			name:          "vacuum_pg16_uses_170000_dir",
			queryName:     "progress/vacuum",
			serverVersion: 160000,
		},
		{
			name:          "vacuum_pg17_uses_base",
			queryName:     "progress/vacuum",
			serverVersion: 170000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := findTemplate(tt.serverVersion, tt.queryName)
			require.NoError(t, err)
			assert.NotEmpty(t, result)

			if tt.wantContains != "" {
				assert.Contains(t, result, tt.wantContains,
					"template for %s at version %d should contain %q",
					tt.queryName, tt.serverVersion, tt.wantContains)
			}
		})
	}
}

func TestFindTemplate_NonexistentQuery(t *testing.T) {
	t.Parallel()

	_, err := findTemplate(170000, "nonexistent/query")
	assert.Error(t, err)
}

// TestFindTemplate_VersionSelectionAlgorithm verifies the exact algorithm behavior:
// keeps dirs > serverVersion, picks MIN. For queries/running (dirs: 90600, 100000):
// - serverVersion < 90600: both qualify → MIN = 90600
// - serverVersion = 90600: only 100000 qualifies (strictly greater)
// - serverVersion = 100000: no dirs qualify → base
// - serverVersion > 100000: no dirs qualify → base
func TestFindTemplate_VersionSelectionAlgorithm(t *testing.T) {
	t.Parallel()

	// For very old server (< 90600), MIN dir > 90500 = 90600
	result90500, err := findTemplate(90500, "queries/running")
	require.NoError(t, err)

	// For PG9.6 (90600), MIN dir > 90600 = 100000
	result90600, err := findTemplate(90600, "queries/running")
	require.NoError(t, err)
	assert.Contains(t, result90600, "NULL::text",
		"PG9.6 should use 100000 dir which has NULL::text AS backend_type")

	// PG9.5 uses 90600/ dir (different from PG9.6 which uses 100000/)
	assert.NotEqual(t, result90500, result90600,
		"PG9.5 and PG9.6 should use different templates")

	// For PG10 (100000), no dirs > 100000 → base template
	result100000, err := findTemplate(100000, "queries/running")
	require.NoError(t, err)
	assert.NotContains(t, result100000, "NULL::text",
		"PG10 should use base template without NULL::text")

	// For PG14 (140000), no dirs qualify → base template
	result140000, err := findTemplate(140000, "queries/running")
	require.NoError(t, err)
	assert.Equal(t, result100000, result140000,
		"PG10 and PG14 should both use base template")
}

// TestFindTemplate_PG17BoundaryBehavior documents the boundary behavior at PG17.
// With the corrected algorithm (MIN dir > serverVersion):
// - PG17 (170000): no dir > 170000 → base (shared_blk_read_time) ✓
// - PG16 (160000): MIN dir > 160000 = 170000 (blk_read_time + temp_blk) ✓
// - PG14 (140000): MIN dir > 140000 = 150000 (blk_read_time only) ✓
func TestFindTemplate_PG17BoundaryBehavior(t *testing.T) {
	t.Parallel()

	// PG17: uses base (has shared_blk_read_time for PG17+)
	pg17, err := findTemplate(170000, "queries/top_10_by_time")
	require.NoError(t, err)
	assert.Contains(t, pg17, "shared_blk_read_time",
		"PG17 uses base template with shared_blk_read_time")

	// PG16: uses 170000/ dir (has blk_read_time + temp_blk_read_time)
	pg16, err := findTemplate(160000, "queries/top_10_by_time")
	require.NoError(t, err)
	assert.Contains(t, pg16, "temp_blk_read_time",
		"PG16 uses 170000 dir with temp_blk_read_time")
	assert.NotContains(t, pg16, "shared_blk_read_time",
		"PG16 should not use shared_blk_read_time")

	// PG14: uses 150000/ dir (blk_read_time only, no temp_blk)
	pg14, err := findTemplate(140000, "queries/top_10_by_time")
	require.NoError(t, err)
	assert.Contains(t, pg14, "blk_read_time")
	assert.NotContains(t, pg14, "temp_blk_read_time",
		"PG14 should not reference temp_blk_read_time")
	assert.NotContains(t, pg14, "shared_blk_read_time",
		"PG14 should not reference shared_blk_read_time")
}
