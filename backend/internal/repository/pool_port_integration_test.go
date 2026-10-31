//go:build integration

package repository

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
)

// tcpProxy forwards one local port to the test container and counts the
// connections it accepted.
type tcpProxy struct {
	ln       net.Listener
	accepted atomic.Int32
}

func startProxy(t *testing.T) *tcpProxy {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	p := &tcpProxy{ln: ln}
	upstream := net.JoinHostPort(tc.Host, tc.Port)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			p.accepted.Add(1)

			go forward(conn, upstream)
		}
	}()

	t.Cleanup(func() {
		_ = ln.Close()
	})

	return p
}

func forward(client net.Conn, upstream string) {
	defer client.Close()

	server, err := net.DialTimeout("tcp", upstream, 5*time.Second)
	if err != nil {
		return
	}

	defer server.Close()

	done := make(chan struct{}, 2)

	go func() { _, _ = io.Copy(server, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, server); done <- struct{}{} }()

	<-done
}

func (p *tcpProxy) port() string {
	_, port, _ := net.SplitHostPort(p.ln.Addr().String())

	return port
}

func publishOnPort(clusters config.Clusters, port string) {
	clusters.UpdateSource("patroni-test", []config.Cluster{{ //nolint:exhaustruct
		Name:      "port_switch",
		UserName:  "test",
		Password:  "test",
		Hosts:     []config.Host{"127.0.0.1"},
		HostMeta:  map[config.Host]config.HostMeta{"127.0.0.1": {Port: port, Alias: "member-1"}},
		Databases: []config.Database{"postgres"},
		Source:    config.SourcePatroni,
	}})
}

func onlyPool(t *testing.T, p *PgxPool) pgxPoolItem {
	t.Helper()

	p.mu.RLock()
	defer p.mu.RUnlock()

	items := p.pools["port_switch"]
	require.Len(t, items, 1)

	return items[0]
}

func TestEnsurePoolReplacesPoolOnPortChange(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	first, second := startProxy(t), startProxy(t)

	clusters := config.NewClustersFromConfig(config.Config{})                  //nolint:exhaustruct
	p := &PgxPool{clusters: clusters, pools: PgxPools{}, logger: zap.NewNop()} //nolint:exhaustruct

	t.Cleanup(func() {
		for _, items := range p.pools {
			for _, item := range items {
				item.pool.Close()
			}
		}
	})

	publishOnPort(clusters, first.port())
	require.NoError(t, p.ensurePool(ctx))

	old := onlyPool(t, p)
	require.Equal(t, first.port(), old.Port)
	require.NoError(t, old.pool.Ping(ctx))
	require.Positive(t, first.accepted.Load())

	publishOnPort(clusters, second.port())
	require.NoError(t, p.ensurePool(ctx))

	cur := onlyPool(t, p)
	require.Equal(t, second.port(), cur.Port)
	require.NotSame(t, old.pool, cur.pool)
	require.NoError(t, cur.pool.Ping(ctx))
	require.Positive(t, second.accepted.Load())

	require.Eventually(t, func() bool {
		conn, err := old.pool.Acquire(context.Background())
		if err == nil {
			conn.Release()
		}

		return err != nil
	}, 5*time.Second, 50*time.Millisecond, "the pool on the old port should be closed")

	// An unchanged port keeps the pool.
	require.NoError(t, p.ensurePool(ctx))
	require.Same(t, cur.pool, onlyPool(t, p).pool)
}
