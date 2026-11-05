package activity

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	// Below one second so viewers polling every second do not miss the cache on timer jitter.
	sampleTTL = 900 * time.Millisecond
	accessTTL = 5 * time.Minute
	idleTTL   = time.Minute
	// Covers the longest poll period of a client with other viewers polling in between.
	recentSamples = 16
)

type Source interface {
	GetActivitySample(ctx context.Context, cluster, instance string, maxSessions int) (Sample, error)
	GetActivityAccess(ctx context.Context, cluster, instance string) (Access, error)
}

type hostKey struct {
	cluster  string
	instance string
}

type hostEntry struct {
	sample    *Sample
	recent    []Sample // oldest first
	fetchedAt time.Time
	access    *Access
	accessAt  time.Time
	lastUsed  time.Time
}

type result struct {
	sample Sample
	access Access
}

// Sampler shares one pg_stat_activity read per host between every caller
// within sampleTTL, and never runs two reads of one host at once.
type Sampler struct {
	src Source
	cfg Config
	now func() time.Time

	group singleflight.Group
	mu    sync.Mutex
	hosts map[hostKey]*hostEntry
}

func NewSampler(src Source, cfg Config) *Sampler {
	return &Sampler{
		src:   src,
		cfg:   cfg.WithDefaults(),
		now:   time.Now,
		hosts: make(map[hostKey]*hostEntry),
	}
}

func (s *Sampler) Get(ctx context.Context, cluster, instance string) (Sample, Access, error) {
	key := hostKey{cluster: cluster, instance: instance}

	if r, ok := s.cached(key); ok {
		return r.sample, r.access, nil
	}

	// The read outlives the caller that started it: other callers wait on it too.
	detached := context.WithoutCancel(ctx)

	ch := s.group.DoChan(cluster+"\x00"+instance, func() (any, error) {
		return s.fetch(detached, key)
	})

	select {
	case res := <-ch:
		if res.Err != nil {
			return Sample{}, Access{}, res.Err
		}

		r := res.Val.(result) //nolint:forcetypeassert // fetch returns only result

		return r.sample, r.access, nil
	case <-ctx.Done():
		return Sample{}, Access{}, ctx.Err()
	}
}

func (s *Sampler) cached(key hostKey) (result, bool) {
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sweep(now)

	e, ok := s.hosts[key]
	if !ok {
		return result{}, false
	}

	e.lastUsed = now

	if e.sample != nil && e.access != nil && now.Sub(e.fetchedAt) < sampleTTL {
		return result{sample: *e.sample, access: *e.access}, true
	}

	return result{}, false
}

func (s *Sampler) fetch(ctx context.Context, key hostKey) (any, error) {
	if r, ok := s.cached(key); ok {
		return r, nil
	}

	access, err := s.accessFor(ctx, key)
	if err != nil {
		return nil, err
	}

	sctx, cancel := context.WithTimeout(ctx, s.cfg.SampleTimeout)
	defer cancel()

	// The sample's age counts from the start of the read: callers pace their polls from their request start.
	started := s.now()

	smp, err := s.src.GetActivitySample(sctx, key.cluster, key.instance, s.cfg.MaxSessions)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	e := s.entry(key, s.now())
	e.sample = &smp
	e.fetchedAt = started
	if len(e.recent) == recentSamples {
		e.recent = append(e.recent[:0], e.recent[1:]...)
	}
	e.recent = append(e.recent, smp)
	s.mu.Unlock()

	return result{sample: smp, access: access}, nil
}

// Recent returns the sample of the host taken at at, while it is still remembered.
func (s *Sampler) Recent(cluster, instance string, at time.Time) (Sample, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.hosts[hostKey{cluster: cluster, instance: instance}]
	if !ok {
		return Sample{}, false
	}

	for i := len(e.recent) - 1; i >= 0; i-- {
		if e.recent[i].At.Equal(at) {
			return e.recent[i], true
		}
	}

	return Sample{}, false
}

// accessFor refreshes the role check every accessTTL; a failed refresh keeps the last known answer.
func (s *Sampler) accessFor(ctx context.Context, key hostKey) (Access, error) {
	now := s.now()

	var (
		prev  *Access
		fresh bool
	)

	s.mu.Lock()
	if e, ok := s.hosts[key]; ok {
		prev = e.access
		fresh = prev != nil && now.Sub(e.accessAt) < accessTTL
	}
	s.mu.Unlock()

	if fresh {
		return *prev, nil
	}

	actx, cancel := context.WithTimeout(ctx, s.cfg.SampleTimeout)
	defer cancel()

	acc, err := s.src.GetActivityAccess(actx, key.cluster, key.instance)
	if err != nil {
		if prev != nil {
			return *prev, nil
		}

		return Access{}, err
	}

	s.mu.Lock()
	e := s.entry(key, now)
	e.access = &acc
	e.accessAt = now
	s.mu.Unlock()

	return acc, nil
}

// entry must be called with s.mu held.
func (s *Sampler) entry(key hostKey, now time.Time) *hostEntry {
	e, ok := s.hosts[key]
	if !ok {
		e = &hostEntry{lastUsed: now}
		s.hosts[key] = e
	}

	return e
}

// sweep must be called with s.mu held.
func (s *Sampler) sweep(now time.Time) {
	for k, e := range s.hosts {
		if now.Sub(e.lastUsed) > idleTTL {
			delete(s.hosts, k)
		}
	}
}
