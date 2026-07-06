package http

import (
	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs"
	"github.com/dbulashev/dasha/internal/metrics"
	"github.com/dbulashev/dasha/internal/repository"
	"github.com/dbulashev/dasha/internal/storage"
)

// Handlers holds the HTTP handler dependencies used by the server.
type Handlers struct {
	cfg     *config.Config
	repo    repository.Repository
	storage *storage.Storage
	metrics *metrics.Service
	logs    logs.Service
}

// NewDashaHandlers constructs a new Handlers instance from its dependencies.
func NewDashaHandlers(cfg *config.Config, repo repository.Repository, st *storage.Storage, ms *metrics.Service, logsSvc logs.Service) *Handlers {
	return &Handlers{cfg: cfg, repo: repo, storage: st, metrics: ms, logs: logsSvc}
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
