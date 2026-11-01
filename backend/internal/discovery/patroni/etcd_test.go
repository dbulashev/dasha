package patroni

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestEtcdStoreDoesNotDialOnCreate(t *testing.T) {
	t.Parallel()

	// With a username clientv3.New authenticates at once; the store must not.
	s, err := newEtcdStore(EtcdConfig{ //nolint:exhaustruct
		Endpoints: []string{"http://127.0.0.1:1"},
		Username:  "dasha_ro",
		Password:  "secret",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("newEtcdStore() error = %v", err)
	}

	if err := s.Close(); err != nil {
		t.Errorf("Close() before first use error = %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	if _, err := s.list(ctx, "/service/"); err == nil {
		t.Fatal("list() error = nil, want an unreachable etcd")
	}

	if s.cli != nil {
		t.Error("a failed connection must not be kept: the next cycle retries")
	}
}
