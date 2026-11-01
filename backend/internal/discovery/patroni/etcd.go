package patroni

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
)

const (
	etcdDialTimeout = 5 * time.Second
	etcdMaxRecvSize = 64 << 20
)

// etcdStore creates the client lazily and recreates it after a failed attempt.
type etcdStore struct {
	cfg clientv3.Config

	mu     sync.Mutex
	cli    *clientv3.Client
	cancel context.CancelFunc // cancels cli's base context
}

func newEtcdStore(cfg EtcdConfig, logger *zap.Logger) (*etcdStore, error) {
	var tlsCfg *tls.Config

	if cfg.TLS.isSet() || usesHTTPS(cfg.Endpoints) {
		var err error

		tlsCfg, err = cfg.TLS.build()
		if err != nil {
			return nil, err
		}
	}

	password, unset := config.ResolveSecret(cfg.Password, cfg.PasswordFromEnv)
	if unset {
		logger.Warn("etcd3.password_from_env is set but the variable is empty")
	}

	return &etcdStore{ //nolint:exhaustruct
		cfg: clientv3.Config{ //nolint:exhaustruct
			Endpoints:          cfg.Endpoints,
			Username:           cfg.Username,
			Password:           password,
			TLS:                tlsCfg,
			DialTimeout:        etcdDialTimeout,
			MaxCallRecvMsgSize: etcdMaxRecvSize,
			Logger:             logger.Named("etcd").WithOptions(zap.IncreaseLevel(zap.WarnLevel)),
		},
	}, nil
}

// client is bounded by DialTimeout and by ctx; the client itself outlives ctx.
func (s *etcdStore) client(ctx context.Context) (*clientv3.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cli != nil {
		return s.cli, nil
	}

	base, cancel := context.WithCancel(context.Background())
	stop := context.AfterFunc(ctx, cancel)

	cfg := s.cfg
	cfg.Context = base

	cli, err := clientv3.New(cfg)

	if !stop() {
		if cli != nil {
			_ = cli.Close()
		}

		return nil, fmt.Errorf("etcd client: %w", context.Cause(ctx))
	}

	if err != nil {
		cancel()

		return nil, fmt.Errorf("etcd client: %w", err)
	}

	s.cli, s.cancel = cli, cancel

	return cli, nil
}

func (s *etcdStore) list(ctx context.Context, prefix string) ([]kv, error) {
	cli, err := s.client(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := cli.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("etcd get %q: %w", prefix, err)
	}

	out := make([]kv, 0, len(resp.Kvs))
	for _, item := range resp.Kvs {
		out = append(out, kv{Key: string(item.Key), Value: item.Value})
	}

	return out, nil
}

func (s *etcdStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cli == nil {
		return nil
	}

	err := s.cli.Close()
	s.cancel()
	s.cli, s.cancel = nil, nil

	if err != nil {
		return fmt.Errorf("etcd close: %w", err)
	}

	return nil
}
