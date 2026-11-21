package http

import (
	"github.com/dbulashev/dasha/internal/activity"
	"github.com/dbulashev/dasha/internal/autosnapshot"
	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/healthscore"
	"github.com/dbulashev/dasha/internal/hotobjects"
	"github.com/dbulashev/dasha/internal/logs"
	"github.com/dbulashev/dasha/internal/metrics"
	"github.com/dbulashev/dasha/internal/pat"
	"github.com/dbulashev/dasha/internal/piicatalog"
	"github.com/dbulashev/dasha/internal/querystats"
	"github.com/dbulashev/dasha/internal/repository"
	"github.com/dbulashev/dasha/internal/statio"
)

// Handlers holds the HTTP handler dependencies used by the server.
type Handlers struct {
	cfg       *config.Config
	repo      repository.Repository
	metrics   *metrics.Service
	logs      logs.Service
	scorer    *healthscore.Scorer
	fleet     *healthscore.Fleet
	activity  *activity.Sampler
	pii       *piicatalog.Reader
	snapshots *autosnapshot.ManualCapturer
	hot       *hotobjects.Reader
	pats      *pat.Service

	tokenAdmin         *pat.Admin
	autosnapshotCfg    *autosnapshot.ConfigService
	autosnapshotStatus *autosnapshot.StatusReader
	queryStats         *querystats.Service
	snapshotCatalog    *querystats.SnapshotCatalog
	ioHistory          *statio.HistoryReader
}

// Services are built in internal/deps. The storage-backed ones are nil when
// storage is not configured, and their endpoints answer 501.
type Services struct {
	Scorer     *healthscore.Scorer
	Fleet      *healthscore.Fleet
	Activity   *activity.Sampler
	PATs       *pat.Service
	TokenAdmin *pat.Admin

	PII                *piicatalog.Reader
	Snapshots          *autosnapshot.ManualCapturer
	Hot                *hotobjects.Reader
	AutosnapshotCfg    *autosnapshot.ConfigService
	AutosnapshotStatus *autosnapshot.StatusReader
	QueryStats         *querystats.Service
	SnapshotCatalog    *querystats.SnapshotCatalog
	IOHistory          *statio.HistoryReader
}

// NewDashaHandlers constructs a new Handlers instance from its dependencies.
func NewDashaHandlers(
	cfg *config.Config, repo repository.Repository, ms *metrics.Service, logsSvc logs.Service, svc Services,
) *Handlers {
	return &Handlers{
		cfg: cfg, repo: repo, metrics: ms, logs: logsSvc,
		scorer:             svc.Scorer,
		fleet:              svc.Fleet,
		activity:           svc.Activity,
		pii:                svc.PII,
		snapshots:          svc.Snapshots,
		hot:                svc.Hot,
		pats:               svc.PATs,
		tokenAdmin:         svc.TokenAdmin,
		autosnapshotCfg:    svc.AutosnapshotCfg,
		autosnapshotStatus: svc.AutosnapshotStatus,
		queryStats:         svc.QueryStats,
		snapshotCatalog:    svc.SnapshotCatalog,
		ioHistory:          svc.IOHistory,
	}
}

// maxLimit caps a client-supplied pagination limit so a SQL LIMIT can never be
// handed a negative or absurdly large value regardless of what the client sends.
const maxLimit = 1000

func paginationDefaults(limitPtr, offsetPtr *int, defaultLimit int) (int, int) {
	limit := defaultLimit
	if limitPtr != nil {
		limit = *limitPtr
	}

	// Clamp to a sane range: a non-positive limit falls back to the per-endpoint
	// default, and an oversized one is capped so the SQL LIMIT stays bounded.
	if limit <= 0 {
		limit = defaultLimit
	}

	if limit > maxLimit {
		limit = maxLimit
	}

	offset := 0
	if offsetPtr != nil {
		offset = *offsetPtr
	}

	// A negative offset is meaningless to SQL OFFSET; floor it at zero.
	if offset < 0 {
		offset = 0
	}

	return limit, offset
}

// emptyIfNil keeps a JSON array an array: a nil slice serializes as null, and a
// client reading a list should not have to handle both spellings of "none".
func emptyIfNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}

	return items
}

// pageOf cuts one page out of a slice, for endpoints that compute the whole
// result before they can filter or count it and so cannot push LIMIT into SQL.
func pageOf[T any](items []T, limit, offset int) []T {
	// Empty, not nil: a page past the end is still an array to whoever reads the
	// JSON, and the helper should not depend on its caller mapping the result.
	if offset >= len(items) {
		return []T{}
	}

	items = items[offset:]
	if len(items) > limit {
		items = items[:limit]
	}

	return items
}
