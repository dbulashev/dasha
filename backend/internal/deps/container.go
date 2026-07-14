package deps

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/samber/do"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/discovery"
	"github.com/dbulashev/dasha/internal/discovery/yandex"
	"github.com/dbulashev/dasha/internal/logs"
	"github.com/dbulashev/dasha/internal/metrics"
	"github.com/dbulashev/dasha/internal/pkg/pat"
	"github.com/dbulashev/dasha/internal/repository"
	"github.com/dbulashev/dasha/internal/storage"
)

type Container struct {
	i *do.Injector
}

func NewContainer() *Container {
	i := do.New()

	do.Provide(i, func(i *do.Injector) (*zap.Logger, error) {
		cfg := do.MustInvoke[*config.Config](i)

		return provideLogger(cfg.Debug), nil
	})

	do.Provide(i, func(_ *do.Injector) (*config.Config, error) {
		cfg, err := provideConfig()

		return cfg, err
	})

	do.Provide(i, func(i *do.Injector) (config.Clusters, error) {
		cfg := do.MustInvoke[*config.Config](i)

		return provideClusters(*cfg), nil
	})

	do.Provide(i, func(i *do.Injector) (repository.Repository, error) {
		cfg := do.MustInvoke[*config.Config](i)
		clusters := do.MustInvoke[config.Clusters](i)
		logger := do.MustInvoke[*zap.Logger](i)

		return provideRepository(*cfg, clusters, logger), nil
	})

	do.Provide(i, func(_ *do.Injector) (*yandex.Registry, error) {
		return yandex.NewRegistry(), nil
	})

	do.Provide(i, func(i *do.Injector) (*discovery.Engine, error) {
		cfg := do.MustInvoke[*config.Config](i)
		clusters := do.MustInvoke[config.Clusters](i)
		registry := do.MustInvoke[*yandex.Registry](i)
		logger := do.MustInvoke[*zap.Logger](i)

		return provideDiscovery(cfg, clusters, registry, logger), nil
	})

	do.Provide(i, func(i *do.Injector) (logs.Service, error) {
		cfg := do.MustInvoke[*config.Config](i)
		clusters := do.MustInvoke[config.Clusters](i)
		registry := do.MustInvoke[*yandex.Registry](i)
		logger := do.MustInvoke[*zap.Logger](i)

		return logs.NewService(clusters, registry, cfg.LogSearch, logger), nil
	})

	do.Provide(i, func(i *do.Injector) (*metrics.Service, error) {
		cfg := do.MustInvoke[*config.Config](i)
		clusters := do.MustInvoke[config.Clusters](i)
		logger := do.MustInvoke[*zap.Logger](i)

		return metrics.NewService(cfg.HealthScore.Metrics, clusterMetaProvider{clusters: clusters}, logger)
	})

	return &Container{i: i}
}

// clusterMetaProvider adapts the cluster registry to metrics.MetadataProvider so
// the matcher can auto-map service-discovered clusters that the static targets[]
// list does not enumerate. Lives here (not in metrics) to keep the metrics
// package free of a config import (config already imports metrics).
type clusterMetaProvider struct {
	clusters config.Clusters
}

func (p clusterMetaProvider) LookupMeta(cluster, instance string) (metrics.DiscoveryMeta, bool) {
	all, err := p.clusters.Get(context.Background())
	if err != nil {
		return metrics.DiscoveryMeta{}, false
	}

	for i := range all {
		c := &all[i]
		if string(c.Name) != cluster {
			continue
		}

		for _, h := range c.Hosts {
			if string(h) == instance {
				return metrics.DiscoveryMeta{
					Source:     c.Source,
					ProviderID: c.ProviderID,
					Labels:     c.Labels,
				}, true
			}
		}
	}

	return metrics.DiscoveryMeta{}, false
}

func (c *Container) Config() *config.Config {
	return do.MustInvoke[*config.Config](c.i)
}

func (c *Container) Logger() *zap.Logger {
	return do.MustInvoke[*zap.Logger](c.i)
}

func (c *Container) Clusters() config.Clusters {
	return do.MustInvoke[config.Clusters](c.i)
}

func (c *Container) Repository() repository.Repository {
	return do.MustInvoke[repository.Repository](c.i)
}

func (c *Container) Discovery() *discovery.Engine {
	return do.MustInvoke[*discovery.Engine](c.i)
}

func (c *Container) Logs() logs.Service {
	return do.MustInvoke[logs.Service](c.i)
}

// Metrics returns the metrics-backed Health Score service, or nil when the
// feature is disabled (the returned *metrics.Service answers Enabled()==false).
func (c *Container) Metrics() *metrics.Service {
	return do.MustInvoke[*metrics.Service](c.i)
}

func (c *Container) AuthMiddlewares(ctx context.Context, resolver auth.PATResolver) (*auth.Middlewares, error) {
	cfg := c.Config()
	logger := c.Logger()

	return auth.NewMiddlewares(ctx, cfg.Auth, resolver, logger)
}

// PAT resolution tuning. The cache keeps the hot path (every API request that
// carries a PAT) off the database, while a short TTL bounds how long a revoked
// token can keep working: revocation only knows the token id, not the secret's
// hash, so it cannot invalidate the cache directly — the TTL is the staleness
// window instead. Negative results are cached briefly so a repeated bad token is
// rejected from memory. A flood of *unique* unknown tokens cannot be absorbed by
// that per-hash cache, so a global lookup limiter caps how many cache-missing
// tokens reach the database per second — the resolver runs before the (optional)
// request rate limiter, so this is the only backstop against a DB-hammering
// flood of random tokens. last_used is updated at most once per touch interval.
const (
	patCacheTTL      = 30 * time.Second
	patNegativeTTL   = 10 * time.Second
	patTouchInterval = 5 * time.Minute
	patMaxCacheSize  = 4096

	// patLookupRPS/patLookupBurst bound DB lookups for tokens not already cached.
	// Legitimate tokens are cached after first use, so steady-state traffic does
	// not touch this; it only throttles first-uses and unknown-token floods. Set
	// generously so normal bursts pass while a random-token flood is capped.
	patLookupRPS   = 50
	patLookupBurst = 100
)

// patEntry is a cached resolution of a presented secret's hash. A nil user marks
// a negative result (unknown/expired/revoked token). touchedAt survives across
// re-resolutions so the last_used throttle holds even though the cache TTL is
// shorter than the touch interval.
type patEntry struct {
	user      *auth.UserContext
	expiresAt time.Time
	touchedAt time.Time
}

// patStore is the storage surface the resolver needs, expressed as an interface
// so the resolver's cache/throttle logic can be unit-tested without a database.
type patStore interface {
	ResolveAPIToken(ctx context.Context, hash []byte) (*storage.APITokenIdentity, bool, error)
	TouchAPIToken(ctx context.Context, hash []byte) error
}

// patResolver adapts the snapshot storage to auth.PATResolver: it hashes the
// presented secret and looks it up, mapping an active token to its owner's
// identity and role. Lives here (not in auth) to keep auth free of a storage
// dependency. Successful resolutions are cached (TTL-bounded) to spare the DB.
type patResolver struct {
	storage    patStore
	logger     *zap.Logger
	ttl        time.Duration
	touchEvery time.Duration

	// lookups caps the rate of DB resolutions for tokens missing from the cache,
	// so a flood of unique unknown tokens cannot hammer the database.
	lookups *rate.Limiter

	mu    sync.Mutex
	cache map[string]*patEntry
}

func (r *patResolver) ResolveToken(ctx context.Context, presented string) (*auth.UserContext, bool) {
	// Fast reject anything that isn't a personal access token before hashing or hitting the DB.
	if !strings.HasPrefix(presented, pat.Prefix) {
		return nil, false
	}

	hash := pat.Hash(presented)
	key := string(hash)

	if user, ok, cached := r.fromCache(ctx, key, hash); cached {
		return user, ok
	}

	// Backstop against a flood of unique unknown tokens: when the global lookup
	// budget is exhausted, fail closed without a DB hit and without caching (the
	// token may be valid — a genuine first-use during a flood — so it must not be
	// poisoned into the negative cache; the client can retry).
	if !r.lookups.Allow() {
		return nil, false
	}

	idn, ok, err := r.storage.ResolveAPIToken(ctx, hash)
	if err != nil {
		// Do not cache backend errors: a transient failure must not lock out a
		// valid token for the negative-TTL window.
		r.logger.Warn("personal access token resolve failed", zap.Error(err))

		return nil, false
	}

	if !ok {
		r.storeNegative(key)

		return nil, false
	}

	user := &auth.UserContext{
		Name:       idn.Subject,
		Role:       idn.Role,
		AuthMethod: auth.MethodPAT,
	}

	if r.storePositive(key, user, idn.ExpiresAt) {
		r.touch(ctx, hash) // first use in this touch window
	}

	return user, true
}

// fromCache returns a cached decision when one is live. cached=false means the
// caller must hit the DB. For a live positive entry it also throttles the
// last_used update to once per touch interval.
func (r *patResolver) fromCache(ctx context.Context, key string, hash []byte) (user *auth.UserContext, ok, cached bool) {
	now := time.Now()

	r.mu.Lock()
	entry, present := r.cache[key]
	if !present || now.After(entry.expiresAt) {
		r.mu.Unlock()

		return nil, false, false
	}

	if entry.user == nil { // live negative result
		r.mu.Unlock()

		return nil, false, true
	}

	needTouch := now.Sub(entry.touchedAt) >= r.touchEvery
	if needTouch {
		entry.touchedAt = now
	}
	user = entry.user
	r.mu.Unlock()

	if needTouch {
		r.touch(ctx, hash)
	}

	return user, true, true
}

// storePositive caches a resolved identity, capping the cache lifetime at the
// token's own expiry so a short-lived token cannot authenticate past it. It
// preserves a prior touchedAt (surviving TTL eviction) so the last_used throttle
// is not defeated by the cache TTL being shorter than the touch interval. It
// returns true when the caller should record a last_used touch now.
func (r *patResolver) storePositive(key string, user *auth.UserContext, tokenExpiry *time.Time) (touch bool) {
	now := time.Now()

	expiresAt := now.Add(r.ttl)
	if tokenExpiry != nil && tokenExpiry.Before(expiresAt) {
		expiresAt = *tokenExpiry
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	touchedAt := now
	touch = true

	if prev, ok := r.cache[key]; ok && prev.user != nil && now.Sub(prev.touchedAt) < r.touchEvery {
		touchedAt = prev.touchedAt // still within the throttle window
		touch = false
	}

	r.evictIfFull(now)
	r.cache[key] = &patEntry{user: user, expiresAt: expiresAt, touchedAt: touchedAt}

	return touch
}

// storeNegative caches an unknown/expired/revoked token so a flood of bad tokens
// is rejected from memory instead of hitting the DB on every request.
func (r *patResolver) storeNegative(key string) {
	now := time.Now()

	r.mu.Lock()
	defer r.mu.Unlock()

	r.evictIfFull(now)
	r.cache[key] = &patEntry{user: nil, expiresAt: now.Add(patNegativeTTL), touchedAt: time.Time{}}
}

// evictIfFull frees a slot once the cache reaches its cap, bounding memory
// without a background sweeper. It prefers to drop entries that cost a legitimate
// user nothing — an expired entry, else a negative (unknown-token) result — and
// evicts a live positive identity only as a last resort. This keeps a burst of
// unknown tokens from pushing cached legitimate users back to the database. One
// eviction is enough: it runs immediately before a single insert. Caller holds r.mu.
func (r *patResolver) evictIfFull(now time.Time) {
	if len(r.cache) < patMaxCacheSize {
		return
	}

	fallback := ""
	haveFallback := false

	for k, e := range r.cache {
		if now.After(e.expiresAt) || e.user == nil {
			delete(r.cache, k)

			return
		}

		if !haveFallback {
			fallback = k
			haveFallback = true
		}
	}

	if haveFallback {
		delete(r.cache, fallback)
	}
}

// touch records last_used best-effort; failures must not block authentication.
func (r *patResolver) touch(ctx context.Context, hash []byte) {
	if err := r.storage.TouchAPIToken(ctx, hash); err != nil {
		r.logger.Debug("personal access token touch failed", zap.Error(err))
	}
}

// NewPATResolver returns an auth.PATResolver backed by storage, or nil (PAT auth
// disabled) when storage is not configured.
func NewPATResolver(st *storage.Storage, logger *zap.Logger) auth.PATResolver {
	if st == nil {
		return nil
	}

	return &patResolver{
		storage:    st,
		logger:     logger,
		ttl:        patCacheTTL,
		touchEvery: patTouchInterval,
		lookups:    rate.NewLimiter(patLookupRPS, patLookupBurst),
		cache:      make(map[string]*patEntry),
	}
}

// NewLoginRecorder returns an auth.LoginRecorder backed by storage, or nil (the
// sign-in audit is disabled) when storage is not configured. The explicit nil —
// rather than passing st straight through — keeps a typed-nil *storage.Storage
// out of the interface, where it would satisfy != nil and panic on first call.
func NewLoginRecorder(st *storage.Storage) auth.LoginRecorder {
	if st == nil {
		return nil
	}

	return st
}

func provideLogger(debug bool) *zap.Logger {
	var (
		l   *zap.Logger
		err error
	)

	if debug {
		l, err = zap.NewDevelopment()
	} else {
		l, err = zap.NewProduction()
	}

	if err != nil {
		panic(err)
	}

	return l
}

func provideConfig() (*config.Config, error) {
	viper.SetConfigName("dasha")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("$HOME/.dasha")
	viper.AddConfigPath("/etc/dasha/")

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("provideConfig | failed to read config: %w", err)
	}

	var c config.Config

	err := viper.Unmarshal(&c)
	if err != nil {
		return nil, fmt.Errorf("provideConfig | %w", err)
	}

	for i := range c.Clusters {
		if c.Clusters[i].PasswordFromEnv != "" {
			c.Clusters[i].Password = os.Getenv(c.Clusters[i].PasswordFromEnv)
		}
	}

	for i := range c.Auth.Tokens {
		if c.Auth.Tokens[i].TokenFromEnv != "" {
			c.Auth.Tokens[i].Token = os.Getenv(c.Auth.Tokens[i].TokenFromEnv)
		}

		if c.Auth.Tokens[i].Role == "" {
			c.Auth.Tokens[i].Role = config.RoleViewer
		}
	}

	if c.Auth.OIDC != nil && c.Auth.OIDC.ClientSecretFromEnv != "" {
		c.Auth.OIDC.ClientSecret = os.Getenv(c.Auth.OIDC.ClientSecretFromEnv)
	}

	if c.Auth.CookieSecretFromEnv != "" {
		c.Auth.CookieSecret = os.Getenv(c.Auth.CookieSecretFromEnv)
	}

	if c.Storage.DSNFromEnv != "" {
		c.Storage.DSN = os.Getenv(c.Storage.DSNFromEnv)
	}

	if c.Storage.DSNMigrationFromEnv != "" {
		c.Storage.DSNMigration = os.Getenv(c.Storage.DSNMigrationFromEnv)
	}

	if env := c.HealthScore.Metrics.Datasource.Auth.TokenFromEnv; env != "" {
		c.HealthScore.Metrics.Datasource.Auth.Token = os.Getenv(env)
	}

	if env := c.HealthScore.Metrics.Datasource.Auth.PasswordFromEnv; env != "" {
		c.HealthScore.Metrics.Datasource.Auth.Password = os.Getenv(env)
	}

	c.HealthScore.Metrics = c.HealthScore.Metrics.WithDefaults()
	c.LogSearch = c.LogSearch.WithDefaults()

	if err := c.HealthScore.Metrics.Validate(); err != nil {
		return nil, fmt.Errorf("provideConfig | health_score.metrics: %w", err)
	}

	if err := c.Auth.Validate(); err != nil {
		return nil, fmt.Errorf("provideConfig | auth config: %w", err)
	}

	return &c, nil
}

func provideClusters(cfg config.Config) config.Clusters {
	return config.NewClustersFromConfig(cfg)
}

func provideRepository(cfg config.Config, clusters config.Clusters, logger *zap.Logger) repository.Repository {
	return repository.NewRepositoryPgxPool(clusters, cfg.PgStatsView, cfg.PgssResetFunction, cfg.DBPool, logger)
}

func provideDiscovery(
	cfg *config.Config,
	clusters config.Clusters,
	registry *yandex.Registry,
	logger *zap.Logger,
) *discovery.Engine {
	if len(cfg.Discovery) == 0 {
		return nil
	}

	return discovery.NewEngine(cfg.Discovery, clusters, registry, logger)
}
