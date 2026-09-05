package auth

import (
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/dbulashev/dasha/internal/config"
)

type rateLimiterStore struct {
	limiters sync.Map // key -> *limiterEntry
	rps      rate.Limit
	burst    int
	stopCh   chan struct{}
}

type limiterEntry struct {
	lim      *rate.Limiter
	lastSeen atomic.Int64 // UnixNano
}

func newRateLimiterStore(rps float64, burst int) *rateLimiterStore {
	s := &rateLimiterStore{
		rps:    rate.Limit(rps),
		burst:  burst,
		stopCh: make(chan struct{}),
	}

	go s.cleanupLoop()

	return s
}

func (s *rateLimiterStore) Stop() {
	close(s.stopCh)
}

func (s *rateLimiterStore) get(key string) *rate.Limiter {
	now := time.Now().UnixNano()

	if val, ok := s.limiters.Load(key); ok {
		entry := val.(*limiterEntry)
		entry.lastSeen.Store(now)

		return entry.lim
	}

	entry := &limiterEntry{lim: rate.NewLimiter(s.rps, s.burst)}
	entry.lastSeen.Store(now)

	actual, _ := s.limiters.LoadOrStore(key, entry)

	return actual.(*limiterEntry).lim
}

const cleanupInterval = 5 * time.Minute
const staleThreshold = 10 * time.Minute

func (s *rateLimiterStore) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			threshold := time.Now().Add(-staleThreshold).UnixNano()

			s.limiters.Range(func(key, value any) bool {
				entry := value.(*limiterEntry)
				if entry.lastSeen.Load() < threshold {
					s.limiters.Delete(key)
				}

				return true
			})
		}
	}
}

type RateLimiter struct {
	Middleware echo.MiddlewareFunc
	store      *rateLimiterStore
}

func (rl *RateLimiter) Stop() {
	if rl.store != nil {
		rl.store.Stop()
	}
}

func NewRateLimiter(cfg config.AuthConfig, logger *zap.Logger) *RateLimiter {
	if cfg.RateLimit == nil || cfg.RateLimit.RequestsPerSecond <= 0 {
		return &RateLimiter{
			Middleware: func(next echo.HandlerFunc) echo.HandlerFunc { return next },
		}
	}

	store := newRateLimiterStore(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)

	mw := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			key := rateLimitKey(c)

			if !store.get(key).Allow() {
				logger.Debug("rate limit exceeded", zap.String("key", key))

				return errRateLimitExceed
			}

			return next(c)
		}
	}

	return &RateLimiter{Middleware: mw, store: store}
}

// PathRateLimiter throttles a set of routes with separate limits for admins and
// everyone else, keyed like the global rate limiter (user name, else client IP).
// Requests are split into groups — one per log source — so a store whose limits
// suit a local index does not inherit the budget of a metered cloud API.
type PathRateLimiter struct {
	Middleware echo.MiddlewareFunc
	stores     []*rateLimiterStore
}

func (p *PathRateLimiter) Stop() {
	for _, s := range p.stores {
		s.Stop()
	}
}

// RateLimitGroup holds the two limits of one group.
type RateLimitGroup struct {
	User, Admin *config.RateLimitConfig
}

type groupStores struct {
	user, admin *rateLimiterStore
}

// NewPathRateLimiter builds a middleware limiting requests to paths. Must run
// after the auth middleware so the admin role is visible. A nil config or
// requests_per_second <= 0 disables the corresponding limit. group names the
// request's group; a name with no configured group falls back to def.
func NewPathRateLimiter(
	paths []string,
	def RateLimitGroup,
	groups map[string]RateLimitGroup,
	group func(echo.Context) string,
	logger *zap.Logger,
) *PathRateLimiter {
	p := &PathRateLimiter{ //nolint:exhaustruct
		stores: nil,
	}

	build := func(g RateLimitGroup) groupStores {
		s := groupStores{user: newStoreFor(g.User), admin: newStoreFor(g.Admin)}

		for _, store := range []*rateLimiterStore{s.user, s.admin} {
			if store != nil {
				p.stores = append(p.stores, store)
			}
		}

		return s
	}

	defStores := build(def)
	byGroup := make(map[string]groupStores, len(groups))

	for name, g := range groups {
		byGroup[name] = build(g)
	}

	p.Middleware = func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Path()
			if !slices.Contains(paths, path) {
				return next(c)
			}

			stores := defStores
			if group != nil {
				if g, ok := byGroup[group(c)]; ok {
					stores = g
				}
			}

			store := stores.user
			if u := GetUser(c); u != nil && u.Role == config.RoleAdmin {
				store = stores.admin
			}

			if store == nil {
				return next(c)
			}

			key := rateLimitKey(c)
			if !store.get(key).Allow() {
				logger.Debug("rate limit exceeded",
					zap.String("path", path),
					zap.String("key", key),
				)

				return errRateLimitExceed
			}

			return next(c)
		}
	}

	return p
}

// newStoreFor builds a limiter store from cfg; nil when the limit is disabled.
// Burst is clamped to >= 1: a zero-burst token bucket would reject everything.
func newStoreFor(cfg *config.RateLimitConfig) *rateLimiterStore {
	if cfg == nil || cfg.RequestsPerSecond <= 0 {
		return nil
	}

	burst := cfg.Burst
	if burst < 1 {
		burst = 1
	}

	return newRateLimiterStore(cfg.RequestsPerSecond, burst)
}

func rateLimitKey(c echo.Context) string {
	if user := GetUser(c); user != nil {
		return "user:" + user.Name
	}

	ip := c.RealIP()
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}

	return "ip:" + ip
}
