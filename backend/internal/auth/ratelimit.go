package auth

import (
	"net"
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

// PathRateLimiter throttles a single route with separate limits for admins and
// everyone else, keyed like the global rate limiter (user name, else client IP).
type PathRateLimiter struct {
	Middleware echo.MiddlewareFunc
	stores     []*rateLimiterStore
}

func (p *PathRateLimiter) Stop() {
	for _, s := range p.stores {
		s.Stop()
	}
}

// NewPathRateLimiter builds a middleware limiting requests to path. Must run
// after the auth middleware so the admin role is visible. A nil config or
// requests_per_second <= 0 disables the corresponding limit.
func NewPathRateLimiter(
	path string,
	userCfg, adminCfg *config.RateLimitConfig,
	logger *zap.Logger,
) *PathRateLimiter {
	userStore := newStoreFor(userCfg)
	adminStore := newStoreFor(adminCfg)

	p := &PathRateLimiter{}
	for _, s := range []*rateLimiterStore{userStore, adminStore} {
		if s != nil {
			p.stores = append(p.stores, s)
		}
	}

	p.Middleware = func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if c.Path() != path {
				return next(c)
			}

			store := userStore
			if u := GetUser(c); u != nil && u.Role == config.RoleAdmin {
				store = adminStore
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
