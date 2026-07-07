package deps

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/pkg/pat"
	"github.com/dbulashev/dasha/internal/storage"
)

// fakePATStore is an in-memory patStore that counts calls so the resolver's
// caching and last_used throttling can be observed without a database.
type fakePATStore struct {
	mu sync.Mutex

	identity *storage.APITokenIdentity // returned when found
	found    bool
	err      error

	resolveCalls int
	touchCalls   int
}

func (f *fakePATStore) ResolveAPIToken(_ context.Context, _ []byte) (*storage.APITokenIdentity, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.resolveCalls++

	return f.identity, f.found, f.err
}

func (f *fakePATStore) TouchAPIToken(_ context.Context, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.touchCalls++

	return nil
}

func (f *fakePATStore) counts() (resolve, touch int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.resolveCalls, f.touchCalls
}

func newResolver(store patStore, ttl, touchEvery time.Duration) *patResolver {
	return &patResolver{
		storage:    store,
		logger:     zap.NewNop(),
		ttl:        ttl,
		touchEvery: touchEvery,
		cache:      make(map[string]*patEntry),
	}
}

func TestResolveToken_RejectsNonPATPrefix(t *testing.T) {
	t.Parallel()

	store := &fakePATStore{found: true, identity: &storage.APITokenIdentity{Subject: "a@b", Role: "viewer"}}
	r := newResolver(store, time.Minute, time.Minute)

	if _, ok := r.ResolveToken(context.Background(), "not-a-pat-secret"); ok {
		t.Fatalf("expected reject for non-PAT prefix")
	}

	if resolve, _ := store.counts(); resolve != 0 {
		t.Errorf("DB hit for non-PAT prefix: resolveCalls=%d, want 0", resolve)
	}
}

func TestResolveToken_MapsIdentityAndMarksMethod(t *testing.T) {
	t.Parallel()

	store := &fakePATStore{found: true, identity: &storage.APITokenIdentity{Subject: "dba@corp", Role: "admin"}}
	r := newResolver(store, time.Minute, time.Minute)

	secret, _, _, _ := pat.Generate()

	user, ok := r.ResolveToken(context.Background(), secret)
	if !ok {
		t.Fatalf("expected resolve to succeed")
	}

	if user.Name != "dba@corp" || user.Role != "admin" {
		t.Errorf("identity = %+v, want subject=dba@corp role=admin", user)
	}

	if user.AuthMethod != auth.MethodPAT {
		t.Errorf("AuthMethod = %v, want MethodPAT", user.AuthMethod)
	}
}

func TestResolveToken_RevokedOrExpiredRejected(t *testing.T) {
	t.Parallel()

	// found=false models a revoked or expired token (ResolveAPIToken filters those).
	store := &fakePATStore{found: false}
	r := newResolver(store, time.Minute, time.Minute)

	secret, _, _, _ := pat.Generate()

	if _, ok := r.ResolveToken(context.Background(), secret); ok {
		t.Fatalf("expected reject for revoked/expired token")
	}
}

func TestResolveToken_CacheAvoidsSecondDBHit(t *testing.T) {
	t.Parallel()

	store := &fakePATStore{found: true, identity: &storage.APITokenIdentity{Subject: "a@b", Role: "viewer"}}
	r := newResolver(store, time.Minute, time.Hour)

	secret, _, _, _ := pat.Generate()

	for range 5 {
		if _, ok := r.ResolveToken(context.Background(), secret); !ok {
			t.Fatalf("expected resolve to succeed")
		}
	}

	resolve, touch := store.counts()
	if resolve != 1 {
		t.Errorf("resolveCalls = %d, want 1 (cached)", resolve)
	}
	// One touch on first use; throttle (1h) suppresses the rest.
	if touch != 1 {
		t.Errorf("touchCalls = %d, want 1 (throttled)", touch)
	}
}

func TestResolveToken_ExpiredCacheReQueries(t *testing.T) {
	t.Parallel()

	store := &fakePATStore{found: true, identity: &storage.APITokenIdentity{Subject: "a@b", Role: "viewer"}}
	r := newResolver(store, time.Nanosecond, time.Hour) // TTL elapses immediately

	secret, _, _, _ := pat.Generate()

	r.ResolveToken(context.Background(), secret)
	time.Sleep(time.Millisecond) // let the entry expire
	r.ResolveToken(context.Background(), secret)

	if resolve, _ := store.counts(); resolve != 2 {
		t.Errorf("resolveCalls = %d, want 2 (cache expired between calls)", resolve)
	}
}

func TestResolveToken_TouchThrottleReleases(t *testing.T) {
	t.Parallel()

	store := &fakePATStore{found: true, identity: &storage.APITokenIdentity{Subject: "a@b", Role: "viewer"}}
	// Long TTL keeps the cache warm; zero touch interval lets every hit touch.
	r := newResolver(store, time.Hour, 0)

	secret, _, _, _ := pat.Generate()

	for range 3 {
		r.ResolveToken(context.Background(), secret)
	}

	resolve, touch := store.counts()
	if resolve != 1 {
		t.Errorf("resolveCalls = %d, want 1 (cached)", resolve)
	}

	if touch != 3 {
		t.Errorf("touchCalls = %d, want 3 (no throttle)", touch)
	}
}

func TestResolveToken_NegativeResultCached(t *testing.T) {
	t.Parallel()

	store := &fakePATStore{found: false}
	// Long negative window via touch interval is irrelevant; patNegativeTTL gates it.
	r := newResolver(store, time.Minute, time.Minute)

	secret, _, _, _ := pat.Generate()

	for range 5 {
		if _, ok := r.ResolveToken(context.Background(), secret); ok {
			t.Fatalf("expected reject for unknown token")
		}
	}

	if resolve, _ := store.counts(); resolve != 1 {
		t.Errorf("resolveCalls = %d, want 1 (negative result cached, no DB flood)", resolve)
	}
}

func TestResolveToken_CacheCappedAtTokenExpiry(t *testing.T) {
	t.Parallel()

	soon := time.Now().Add(20 * time.Millisecond)
	store := &fakePATStore{
		found:    true,
		identity: &storage.APITokenIdentity{Subject: "a@b", Role: "viewer", ExpiresAt: &soon},
	}
	// TTL is long, but the token expires in 20ms — the cache must not outlive it.
	r := newResolver(store, time.Hour, time.Hour)

	secret, _, _, _ := pat.Generate()

	r.ResolveToken(context.Background(), secret)
	time.Sleep(40 * time.Millisecond) // token (and thus the cache entry) has expired
	r.ResolveToken(context.Background(), secret)

	if resolve, _ := store.counts(); resolve != 2 {
		t.Errorf("resolveCalls = %d, want 2 (cache expiry capped at token expiry)", resolve)
	}
}

func TestNewPATResolver_NilWithoutStorage(t *testing.T) {
	t.Parallel()

	if r := NewPATResolver(nil, zap.NewNop()); r != nil {
		t.Errorf("NewPATResolver(nil) = %v, want nil (PAT disabled)", r)
	}
}
