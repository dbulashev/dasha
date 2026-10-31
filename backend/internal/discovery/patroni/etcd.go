package patroni

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
)

const (
	etcdDialTimeout = 5 * time.Second
	etcdMaxRecvSize = 64 << 20
)

type etcdStore struct {
	cli *clientv3.Client
}

// newEtcdStore does not wait for a connection; the first list reports an unreachable etcd.
func newEtcdStore(cfg EtcdConfig, logger *zap.Logger) (*etcdStore, error) {
	var tlsCfg *tls.Config

	if cfg.TLS.isSet() || usesHTTPS(cfg.Endpoints) {
		var err error

		tlsCfg, err = cfg.TLS.build()
		if err != nil {
			return nil, err
		}
	}

	cli, err := clientv3.New(clientv3.Config{ //nolint:exhaustruct
		Endpoints:          cfg.Endpoints,
		Username:           cfg.Username,
		Password:           secret(cfg.Password, cfg.PasswordFromEnv),
		TLS:                tlsCfg,
		DialTimeout:        etcdDialTimeout,
		MaxCallRecvMsgSize: etcdMaxRecvSize,
		Logger:             logger.Named("etcd").WithOptions(zap.IncreaseLevel(zap.WarnLevel)),
	})
	if err != nil {
		return nil, fmt.Errorf("etcd client: %w", err)
	}

	return &etcdStore{cli: cli}, nil
}

func (s *etcdStore) list(ctx context.Context, prefix string) ([]kv, error) {
	resp, err := s.cli.Get(ctx, prefix, clientv3.WithPrefix())
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
	if err := s.cli.Close(); err != nil {
		return fmt.Errorf("etcd close: %w", err)
	}

	return nil
}
