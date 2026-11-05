package activity

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSource struct {
	samples  atomic.Int32
	accesses atomic.Int32
	release  chan struct{} // nil: answer at once
	err      error
	accErr   error
	sawLimit atomic.Int32
	onSample func() // runs inside the read, e.g. to move the clock
	seq      atomic.Int64
}

func (f *fakeSource) GetActivitySample(ctx context.Context, _, _ string, maxSessions int) (Sample, error) {
	f.samples.Add(1)
	f.sawLimit.Store(int32(maxSessions))

	if f.onSample != nil {
		f.onSample()
	}

	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return Sample{}, ctx.Err()
		}
	}

	if f.err != nil {
		return Sample{}, f.err
	}

	return Sample{At: time.Unix(0, f.seq.Add(1)), Sessions: []Session{{PID: 1}}}, nil
}

func (f *fakeSource) GetActivityAccess(context.Context, string, string) (Access, error) {
	f.accesses.Add(1)

	if f.accErr != nil {
		return Access{}, f.accErr
	}

	return Access{CanReadAllStats: true}, nil
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newTestSampler(src Source) (*Sampler, *clock) {
	c := &clock{t: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
	s := NewSampler(src, Config{})
	s.now = c.now

	return s, c
}

func TestSamplerSharesOneReadWithinTTL(t *testing.T) {
	t.Parallel()

	src := &fakeSource{release: make(chan struct{})}
	s, _ := newTestSampler(src)

	var wg sync.WaitGroup

	for range 6 {
		wg.Go(func() {
			_, acc, err := s.Get(t.Context(), "c", "h")
			assert.NoError(t, err)
			assert.True(t, acc.CanReadAllStats)
		})
	}

	require.Eventually(t, func() bool { return src.samples.Load() == 1 }, time.Second, time.Millisecond)
	close(src.release)
	wg.Wait()

	_, _, err := s.Get(t.Context(), "c", "h")
	require.NoError(t, err)

	assert.EqualValues(t, 1, src.samples.Load())
	assert.EqualValues(t, 1, src.accesses.Load())
	assert.EqualValues(t, DefaultMaxSessions, src.sawLimit.Load())
}

func TestSamplerRefreshesAfterTTL(t *testing.T) {
	t.Parallel()

	src := &fakeSource{}
	s, c := newTestSampler(src)

	_, _, err := s.Get(t.Context(), "c", "h")
	require.NoError(t, err)

	c.advance(sampleTTL)

	_, _, err = s.Get(t.Context(), "c", "h")
	require.NoError(t, err)

	assert.EqualValues(t, 2, src.samples.Load())
	assert.EqualValues(t, 1, src.accesses.Load(), "access is checked every accessTTL, not every sample")

	c.advance(accessTTL)

	_, _, err = s.Get(t.Context(), "c", "h")
	require.NoError(t, err)
	assert.EqualValues(t, 2, src.accesses.Load())
}

func TestSamplerSlowReadDoesNotServeNextPollFromCache(t *testing.T) {
	t.Parallel()

	src := &fakeSource{}
	s, c := newTestSampler(src)
	src.onSample = func() { c.advance(300 * time.Millisecond) }

	_, _, err := s.Get(t.Context(), "c", "h")
	require.NoError(t, err)

	// The next poll comes one second after the previous request started.
	c.advance(time.Second - 300*time.Millisecond)

	_, _, err = s.Get(t.Context(), "c", "h")
	require.NoError(t, err)

	assert.EqualValues(t, 2, src.samples.Load())
}

func TestSamplerRemembersRecentSamples(t *testing.T) {
	t.Parallel()

	src := &fakeSource{}
	s, c := newTestSampler(src)

	var ats []time.Time

	for range recentSamples + 1 {
		smp, _, err := s.Get(t.Context(), "c", "h")
		require.NoError(t, err)

		ats = append(ats, smp.At)
		c.advance(sampleTTL)
	}

	_, ok := s.Recent("c", "h", ats[0])
	assert.False(t, ok, "the oldest sample fell out of the ring")

	got, ok := s.Recent("c", "h", ats[len(ats)-1])
	require.True(t, ok)
	assert.Equal(t, ats[len(ats)-1], got.At)

	_, ok = s.Recent("c", "other", ats[1])
	assert.False(t, ok)
}

func TestSamplerCallerCancelDoesNotCancelRead(t *testing.T) {
	t.Parallel()

	src := &fakeSource{release: make(chan struct{})}
	s, _ := newTestSampler(src)

	first, cancel := context.WithCancel(t.Context())
	firstDone := make(chan error, 1)

	go func() {
		_, _, err := s.Get(first, "c", "h")
		firstDone <- err
	}()

	require.Eventually(t, func() bool { return src.samples.Load() == 1 }, time.Second, time.Millisecond)

	secondDone := make(chan error, 1)

	go func() {
		_, _, err := s.Get(t.Context(), "c", "h")
		secondDone <- err
	}()

	cancel()
	require.ErrorIs(t, <-firstDone, context.Canceled)

	close(src.release)
	require.NoError(t, <-secondDone)
	assert.EqualValues(t, 1, src.samples.Load())
}

func TestSamplerDoesNotCacheErrors(t *testing.T) {
	t.Parallel()

	src := &fakeSource{err: errors.New("boom")}
	s, _ := newTestSampler(src)

	_, _, err := s.Get(t.Context(), "c", "h")
	require.Error(t, err)

	_, _, err = s.Get(t.Context(), "c", "h")
	require.Error(t, err)

	assert.EqualValues(t, 2, src.samples.Load())
}

func TestSamplerSweepsIdleHosts(t *testing.T) {
	t.Parallel()

	src := &fakeSource{}
	s, c := newTestSampler(src)

	_, _, err := s.Get(t.Context(), "c", "a")
	require.NoError(t, err)

	c.advance(idleTTL + time.Second)

	_, _, err = s.Get(t.Context(), "c", "b")
	require.NoError(t, err)

	s.mu.Lock()
	defer s.mu.Unlock()

	assert.Len(t, s.hosts, 1)
	assert.Contains(t, s.hosts, hostKey{cluster: "c", instance: "b"})
}

func TestSamplerKeepsNoEntryForUnknownHosts(t *testing.T) {
	t.Parallel()

	src := &fakeSource{accErr: errors.New("not found")}
	s, _ := newTestSampler(src)

	for _, h := range []string{"x", "y", "z"} {
		_, _, err := s.Get(t.Context(), "c", h)
		require.Error(t, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	assert.Empty(t, s.hosts)
}
