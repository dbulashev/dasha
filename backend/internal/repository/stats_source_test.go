package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

const (
	pgssExt   = "pg_stat_statements"
	pgproExt  = "pgpro_stats"
	testPG    = 170000
	testExtNs = `"ext"`
)

// stubResolver builds a resolver over canned catalog and probe answers.
type stubResolver struct {
	installed  map[string]catalogExt
	catalogErr error
	readable   map[string]bool
	probeErr   error
	now        time.Time

	catalogCalls int
	probed       []string
}

func (st *stubResolver) resolver() statsSourceResolver {
	return statsSourceResolver{
		catalog: func(context.Context) (map[string]catalogExt, error) {
			st.catalogCalls++

			return st.installed, st.catalogErr
		},
		readable: func(_ context.Context, relation string) (bool, error) {
			st.probed = append(st.probed, relation)

			if st.probeErr != nil {
				return false, st.probeErr
			}

			return st.readable[relation], nil
		},
		now:    func() time.Time { return st.now },
		cache:  &sync.Map{},
		key:    "pool",
		logger: zap.NewNop(),
	}
}

func bare(names ...string) map[string]catalogExt {
	m := make(map[string]catalogExt, len(names))
	for _, n := range names {
		m[n] = catalogExt{schema: "", version: "1.11"}
	}

	return m
}

func TestStatsSource_Selection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		installed  map[string]catalogExt
		readable   map[string]bool
		wantSource string
		wantFound  bool
		wantView   string
	}{
		{
			name:       "only pg_stat_statements",
			installed:  bare(pgssExt),
			wantSource: pgssExt,
			wantFound:  true,
			wantView:   "pg_stat_statements",
		},
		{
			name:       "only pgpro_stats",
			installed:  bare(pgproExt),
			wantSource: pgproExt,
			wantFound:  true,
			wantView:   "pgpro_stats_statements",
		},
		{
			name:       "neither installed falls back to pg_stat_statements names",
			installed:  bare(),
			wantSource: pgssExt,
			wantFound:  false,
			wantView:   "pg_stat_statements",
		},
		{
			name:       "both readable prefers pg_stat_statements",
			installed:  bare(pgssExt, pgproExt),
			readable:   map[string]bool{"pg_stat_statements": true, "pgpro_stats_statements": true},
			wantSource: pgssExt,
			wantFound:  true,
			wantView:   "pg_stat_statements",
		},
		{
			name:       "only pgpro_stats reads wins despite lower priority",
			installed:  bare(pgssExt, pgproExt),
			readable:   map[string]bool{"pg_stat_statements": false, "pgpro_stats_statements": true},
			wantSource: pgproExt,
			wantFound:  true,
			wantView:   "pgpro_stats_statements",
		},
		{
			name:       "neither reads keeps the priority pick",
			installed:  bare(pgssExt, pgproExt),
			readable:   map[string]bool{},
			wantSource: pgssExt,
			wantFound:  true,
			wantView:   "pg_stat_statements",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := &stubResolver{installed: tt.installed, readable: tt.readable, now: time.Now()} //nolint:exhaustruct
			src := st.resolver().resolve(t.Context())

			assert.Equal(t, tt.wantFound, src.Present())
			assert.Equal(t, tt.wantSource, src.Name())
			assert.Equal(t, tt.wantView, src.Relation())
		})
	}
}

func TestStatsSource_QualifiesWithSchema(t *testing.T) {
	t.Parallel()

	st := &stubResolver{ //nolint:exhaustruct
		installed: map[string]catalogExt{pgproExt: {schema: testExtNs, version: "1.10"}},
		now:       time.Now(),
	}

	src := st.resolver().resolve(t.Context())

	assert.Equal(t, `"ext".pgpro_stats_statements`, src.Relation())
	assert.Equal(t, `"ext".pgpro_stats_info`, src.InfoRelation())
	assert.Equal(t, `"ext".pgpro_stats_statements_reset`, src.ResetFunc())
}

func TestStatsSource_NoProbeUnlessBothInstalled(t *testing.T) {
	t.Parallel()

	st := &stubResolver{installed: bare(pgproExt), now: time.Now()} //nolint:exhaustruct
	st.resolver().resolve(t.Context())

	assert.Empty(t, st.probed)
}

func TestStatsSource_CacheTTL(t *testing.T) {
	t.Parallel()

	t.Run("a hit is held for the long TTL", func(t *testing.T) {
		t.Parallel()

		start := time.Now()
		st := &stubResolver{installed: bare(pgssExt), now: start} //nolint:exhaustruct
		r := st.resolver()

		r.resolve(t.Context())
		st.now = start.Add(extSchemaHitTTL - time.Second)
		r.resolve(t.Context())
		assert.Equal(t, 1, st.catalogCalls, "still inside the hit TTL")

		st.now = start.Add(extSchemaHitTTL + time.Second)
		r.resolve(t.Context())
		assert.Equal(t, 2, st.catalogCalls, "expired hit is resolved again")
	})

	t.Run("a miss expires on the short TTL", func(t *testing.T) {
		t.Parallel()

		start := time.Now()
		st := &stubResolver{installed: bare(), now: start} //nolint:exhaustruct
		r := st.resolver()

		r.resolve(t.Context())
		st.now = start.Add(extSchemaMissTTL + time.Second)

		st.installed = bare(pgproExt)
		src := r.resolve(t.Context())

		assert.Equal(t, 2, st.catalogCalls)
		assert.Equal(t, pgproExt, src.Name(), "CREATE EXTENSION takes effect without a restart")
	})

	t.Run("a catalog error is not pinned for an hour", func(t *testing.T) {
		t.Parallel()

		start := time.Now()
		st := &stubResolver{catalogErr: errors.New("connection reset"), now: start} //nolint:exhaustruct
		r := st.resolver()

		assert.False(t, r.resolve(t.Context()).Present())

		st.now = start.Add(extSchemaMissTTL + time.Second)
		st.catalogErr = nil
		st.installed = bare(pgssExt)

		assert.True(t, r.resolve(t.Context()).Present())
	})

	t.Run("a failed probe is not pinned either", func(t *testing.T) {
		t.Parallel()

		start := time.Now()
		st := &stubResolver{ //nolint:exhaustruct
			installed: bare(pgssExt, pgproExt),
			probeErr:  errors.New("connection reset"),
			now:       start,
		}
		r := st.resolver()

		assert.Equal(t, pgssExt, r.resolve(t.Context()).Name())

		st.now = start.Add(extSchemaMissTTL + time.Second)
		st.probeErr = nil
		st.readable = map[string]bool{"pgpro_stats_statements": true}

		assert.Equal(t, pgproExt, r.resolve(t.Context()).Name())
	})
}

// Source resolution feeds the data templates nothing but these two names, so
// asserting them covers every rendered query.
func TestStatsSource_VanillaSQLUnchanged(t *testing.T) {
	t.Parallel()

	st := &stubResolver{installed: bare(pgssExt), now: time.Now()} //nolint:exhaustruct
	src := st.resolver().resolve(t.Context())
	data := pgssTemplateData{Pgss: src.Relation(), PgssInfo: src.InfoRelation()}

	require.Equal(t, "pg_stat_statements", data.Pgss)
	require.Equal(t, "pg_stat_statements_info", data.PgssInfo)

	queries := []enums.Query{
		enums.QueryQueriesTop10ByTime,
		enums.QueryQueriesTop10ByWal,
		enums.QueryQueriesTop10Chart,
		enums.QueryQueriesReport,
		enums.QueryDatabasePgssStatsResetTime,
		enums.QueryCommonQueryStatsReadable,
		enums.QueryIndexAdvisorWorkload,
	}

	// Every supported major, so version-specific overrides render too.
	versions := []int{140000, 150000, 160000, 170000, 180000}

	for _, q := range queries {
		for _, v := range versions {
			sql, err := query.Get(v, q, data)
			require.NoError(t, err, "%s @ %d", q, v)

			assert.Contains(t, sql, "pg_stat_statements", "%s @ %d", q, v)
			assert.NotContains(t, sql, "pgpro", "%s @ %d", q, v)
			assert.NotContains(t, sql, "{{", "%s @ %d: template must be fully rendered", q, v)
		}
	}
}

func TestStatsSource_PgproSQLSubstitution(t *testing.T) {
	t.Parallel()

	st := &stubResolver{ //nolint:exhaustruct
		installed: map[string]catalogExt{pgproExt: {schema: testExtNs, version: "1.10"}},
		now:       time.Now(),
	}
	src := st.resolver().resolve(t.Context())
	data := pgssTemplateData{Pgss: src.Relation(), PgssInfo: src.InfoRelation()}

	sql, err := query.Get(testPG, enums.QueryQueriesReport, data)
	require.NoError(t, err)
	assert.Contains(t, sql, `"ext".pgpro_stats_statements`)
	assert.NotContains(t, sql, "FROM pg_stat_statements")

	info, err := query.Get(testPG, enums.QueryDatabasePgssStatsResetTime, data)
	require.NoError(t, err)
	assert.Contains(t, info, `"ext".pgpro_stats_info`)
}

func TestStatsSource_StatusProbeTemplates(t *testing.T) {
	t.Parallel()

	available, err := query.Get(testPG, enums.QueryCommonQueryStatsAvailable, extensionTemplateData{Extension: pgssExt})
	require.NoError(t, err)
	assert.Equal(t,
		"SELECT COUNT(*) > 0 AS available FROM pg_available_extensions WHERE name = 'pg_stat_statements'\n",
		available)

	enabled, err := query.Get(testPG, enums.QueryCommonQueryStatsEnabled, extensionTemplateData{Extension: pgssExt})
	require.NoError(t, err)
	assert.Equal(t,
		"SELECT COUNT(*) > 0 AS count FROM pg_extension WHERE extname = 'pg_stat_statements'\n",
		enabled)

	pgproAvailable, err := query.Get(testPG, enums.QueryCommonQueryStatsAvailable, extensionTemplateData{Extension: pgproExt})
	require.NoError(t, err)
	assert.Contains(t, pgproAvailable, "'pgpro_stats'")

	pgproEnabled, err := query.Get(testPG, enums.QueryCommonQueryStatsEnabled, extensionTemplateData{Extension: pgproExt})
	require.NoError(t, err)
	assert.Contains(t, pgproEnabled, "'pgpro_stats'")
}

func TestStatsSource_ResetFunction(t *testing.T) {
	t.Parallel()

	pgpro := &stubResolver{ //nolint:exhaustruct
		installed: map[string]catalogExt{pgproExt: {schema: testExtNs, version: "1.10"}},
		now:       time.Now(),
	}
	fallback := func() string { return pgpro.resolver().resolve(t.Context()).ResetFunc() }
	logger := zap.NewNop()

	assert.Equal(t, `"ext".pgpro_stats_statements_reset`, resetFunction("", fallback, logger))
	assert.Equal(t, "monitoring.reset_pgss", resetFunction("monitoring.reset_pgss", fallback, logger))
	assert.Equal(t, `"ext".pgpro_stats_statements_reset`, resetFunction("drop table x; --", fallback, logger),
		"an invalid override falls back to the source's own function")

	vanilla := &stubResolver{installed: bare(pgssExt), now: time.Now()} //nolint:exhaustruct
	assert.Equal(t, "pg_stat_statements_reset",
		resetFunction("", func() string { return vanilla.resolver().resolve(t.Context()).ResetFunc() }, logger))
}

func TestStatsSource_RecommendationOrder(t *testing.T) {
	t.Parallel()

	require.Len(t, recommendedSourceOrder, len(statsSourceDefs))
	assert.Equal(t, pgproExt, recommendedSourceOrder[0].Ext)
	assert.Equal(t, pgssExt, recommendedSourceOrder[1].Ext)
	assert.Equal(t, pgssExt, statsSourceDefs[0].Ext, "conflict priority stays on pg_stat_statements")
}
