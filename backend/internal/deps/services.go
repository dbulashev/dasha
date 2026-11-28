package deps

import (
	"github.com/samber/do"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/activity"
	"github.com/dbulashev/dasha/internal/autosnapshot"
	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/healthscore"
	"github.com/dbulashev/dasha/internal/hotobjects"
	dashahttp "github.com/dbulashev/dasha/internal/http"
	"github.com/dbulashev/dasha/internal/metrics"
	"github.com/dbulashev/dasha/internal/pat"
	"github.com/dbulashev/dasha/internal/piicatalog"
	"github.com/dbulashev/dasha/internal/querystats"
	"github.com/dbulashev/dasha/internal/repository"
	"github.com/dbulashev/dasha/internal/statio"
	"github.com/dbulashev/dasha/internal/statlock"
	"github.com/dbulashev/dasha/internal/storage"
)

// provideServices registers the domain services. Scorer, Fleet and Sampler
// keep caches and concurrency limits, so every consumer has to share one
// instance. All of them need ProvideStorage to have run.
func provideServices(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*healthscore.Scorer, error) {
		cfg := do.MustInvoke[*config.Config](i)
		repo := do.MustInvoke[repository.Repository](i)
		ms := do.MustInvoke[*metrics.Service](i)

		// A typed-nil *storage.Storage must not end up in a store interface.
		var store healthscore.Store
		if st := do.MustInvoke[*storage.Storage](i); st != nil {
			store = st
		}

		return healthscore.NewScorer(cfg, repo, store, ms), nil
	})

	do.Provide(i, func(i *do.Injector) (*healthscore.Fleet, error) {
		cfg := do.MustInvoke[*config.Config](i)
		scorer := do.MustInvoke[*healthscore.Scorer](i)
		ms := do.MustInvoke[*metrics.Service](i)
		repo := do.MustInvoke[repository.Repository](i)
		logger := do.MustInvoke[*zap.Logger](i)

		return healthscore.NewFleet(cfg, scorer, ms, repo, logger), nil
	})

	do.Provide(i, func(i *do.Injector) (*activity.Sampler, error) {
		cfg := do.MustInvoke[*config.Config](i)
		repo := do.MustInvoke[repository.Repository](i)

		return activity.NewSampler(repo, cfg.Activity), nil
	})

	do.Provide(i, func(i *do.Injector) (dashahttp.Services, error) {
		return newHandlerServices(i), nil
	})
}

// HandlerServices returns the services behind the HTTP handlers. It has to run
// after ProvideStorage.
func (c *Container) HandlerServices() dashahttp.Services {
	return do.MustInvoke[dashahttp.Services](c.i)
}

// newHandlerServices leaves the storage-backed services nil when storage is
// not configured.
func newHandlerServices(i *do.Injector) dashahttp.Services {
	cfg := do.MustInvoke[*config.Config](i)
	repo := do.MustInvoke[repository.Repository](i)
	clusters := do.MustInvoke[config.Clusters](i)
	logger := do.MustInvoke[*zap.Logger](i)
	st := do.MustInvoke[*storage.Storage](i)

	// A typed-nil *storage.Storage must not end up in a store interface.
	var (
		patStore   pat.Store
		adminStore pat.AdminStore
	)

	if st != nil {
		patStore = st
		adminStore = st
	}

	svc := dashahttp.Services{
		Scorer:     do.MustInvoke[*healthscore.Scorer](i),
		Fleet:      do.MustInvoke[*healthscore.Fleet](i),
		Activity:   do.MustInvoke[*activity.Sampler](i),
		PATs:       pat.NewService(patStore, cfg.Auth.PATMinRole),
		TokenAdmin: pat.NewAdmin(adminStore),
	}

	if st == nil {
		return svc
	}

	svc.PII = piicatalog.NewReader(st, clusters, cfg.PIICatalog.Schedule)
	svc.Snapshots = autosnapshot.NewManualCapturer(autosnapshot.NewCapturer(repo, st, logger), st)
	svc.Hot = hotobjects.NewReader(st, repo, logger)
	svc.AutosnapshotCfg = autosnapshot.NewConfigService(st, repo)
	svc.AutosnapshotStatus = autosnapshot.NewStatusReader(st)
	svc.QueryStats = querystats.NewService(st, repo)
	svc.SnapshotCatalog = querystats.NewSnapshotCatalog(st)
	svc.IOHistory = statio.NewHistoryReader(st)
	svc.LockHistory = statlock.NewHistoryReader(st)

	return svc
}
