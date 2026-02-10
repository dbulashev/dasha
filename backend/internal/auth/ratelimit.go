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
