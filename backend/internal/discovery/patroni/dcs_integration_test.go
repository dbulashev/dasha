//go:build integration

package patroni

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/testinfra"
)

const (
	etcdRootPassword = "rootpw"
	etcdROUser       = "dasha_ro"
	etcdROPassword   = "dasha_ro"
	consulRootToken  = "11111111-2222-3333-4444-555555555555"
	consulROToken    = "00000000-0000-0000-0000-00000000da5a"
	// Every test namespace lives under this one; the read-only grants cover it.
	itNamespace = "it"
	// Resolves nowhere: a member PostgreSQL never answers on.
	deadHost = "dasha-patroni-nowhere.invalid"
)

var (
	tc         *testinfra.TestContainer
	etcdURL    string
	etcdWriter *clientv3.Client
	consulURL  string
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	tc = testinfra.MustNew(ctx)

	etcdC := mustStartEtcd(ctx)
	consulC := mustStartConsul(ctx)

	code := m.Run()

	_ = etcdWriter.Close()
	_ = etcdC.Terminate(ctx)
	_ = consulC.Terminate(ctx)

	tc.TearDown(ctx)
	os.Exit(code)
}

func imageFromEnv(env, def string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}

	return def
}

func mappedURL(ctx context.Context, c testcontainers.Container, port string) string {
	host, err := c.Host(ctx)
	if err != nil {
		panic(err)
	}

	mapped, err := c.MappedPort(ctx, port)
	if err != nil {
		panic(err)
	}

	return fmt.Sprintf("http://%s:%s", host, mapped.Port())
}

// mustStartEtcd starts etcd with auth on: root writes, dasha_ro may only read
// the test namespace, as in the documented role setup.
func mustStartEtcd(ctx context.Context) testcontainers.Container {
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        imageFromEnv("ETCD_IMAGE", "quay.io/coreos/etcd:v3.6.4"),
			ExposedPorts: []string{"2379/tcp"},
			Cmd: []string{
				"etcd",
				"--name", "it",
				"--data-dir", "/tmp/etcd",
				"--listen-client-urls", "http://0.0.0.0:2379",
				"--advertise-client-urls", "http://127.0.0.1:2379",
			},
			WaitingFor: wait.ForHTTP("/health").WithPort("2379/tcp").WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	if err != nil {
		panic(fmt.Sprintf("start etcd: %v", err))
	}

	etcdURL = mappedURL(ctx, c, "2379/tcp")

	admin, err := clientv3.New(clientv3.Config{Endpoints: []string{etcdURL}, DialTimeout: 5 * time.Second}) //nolint:exhaustruct
	if err != nil {
		panic(err)
	}

	prefix := "/" + itNamespace + "/"
	steps := []func() error{
		func() error { _, err := admin.UserAdd(ctx, "root", etcdRootPassword); return err },
		func() error { _, err := admin.UserGrantRole(ctx, "root", "root"); return err },
		func() error { _, err := admin.RoleAdd(ctx, etcdROUser); return err },
		func() error {
			_, err := admin.RoleGrantPermission(ctx, etcdROUser, prefix, clientv3.GetPrefixRangeEnd(prefix),
				clientv3.PermissionType(clientv3.PermRead))
			return err
		},
		func() error { _, err := admin.UserAdd(ctx, etcdROUser, etcdROPassword); return err },
		func() error { _, err := admin.UserGrantRole(ctx, etcdROUser, etcdROUser); return err },
		func() error { _, err := admin.AuthEnable(ctx); return err },
	}

	for i, step := range steps {
		if err := step(); err != nil {
			panic(fmt.Sprintf("etcd auth setup step %d: %v", i, err))
		}
	}

	_ = admin.Close()

	etcdWriter, err = clientv3.New(clientv3.Config{ //nolint:exhaustruct
		Endpoints:   []string{etcdURL},
		Username:    "root",
		Password:    etcdRootPassword,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		panic(err)
	}

	return c
}

// mustStartConsul starts a dev agent with ACLs denying by default and a
// read-only token for the test namespace.
func mustStartConsul(ctx context.Context) testcontainers.Container {
	acl := fmt.Sprintf(`{"acl": {"enabled": true, "default_policy": "deny",`+
		` "tokens": {"initial_management": %q}}}`, consulRootToken)

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        imageFromEnv("CONSUL_IMAGE", "hashicorp/consul:1.21"),
			ExposedPorts: []string{"8500/tcp"},
			Env:          map[string]string{"CONSUL_LOCAL_CONFIG": acl},
			Cmd:          []string{"agent", "-dev", "-client", "0.0.0.0"},
			WaitingFor: wait.ForHTTP("/v1/status/leader").WithPort("8500/tcp").
				WithResponseMatcher(func(body io.Reader) bool {
					b, _ := io.ReadAll(body)
					return len(bytes.Trim(b, "\"\n ")) > 0
				}).
				WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	if err != nil {
		panic(fmt.Sprintf("start consul: %v", err))
	}

	consulURL = mappedURL(ctx, c, "8500/tcp")

	policy := map[string]any{
		"Name":  "dasha-ro",
		"Rules": fmt.Sprintf(`key_prefix %q { policy = "read" }`, itNamespace+"/"),
	}
	token := map[string]any{
		"SecretID": consulROToken,
		"Policies": []map[string]string{{"Name": "dasha-ro"}},
	}

	// The ACL system needs a moment after the leader is elected.
	deadline := time.Now().Add(time.Minute)

	for _, step := range []struct {
		path string
		body any
	}{{"/v1/acl/policy", policy}, {"/v1/acl/token", token}} {
		for {
			status, err := consulDo(ctx, http.MethodPut, step.path, consulRootToken, step.body)
			if err == nil && status == http.StatusOK {
				break
			}

			if time.Now().After(deadline) {
				panic(fmt.Sprintf("consul %s: status %d, err %v", step.path, status, err))
			}

			time.Sleep(500 * time.Millisecond)
		}
	}

	return c
}

func consulDo(ctx context.Context, method, path, token string, body any) (int, error) {
	var payload io.Reader = http.NoBody

	switch b := body.(type) {
	case nil:
	case string:
		payload = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return 0, err
		}

		payload = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, consulURL+path, payload)
	if err != nil {
		return 0, err
	}

	req.Header.Set(consulTokenHdr, token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	return resp.StatusCode, nil
}

// dcsBackend writes Patroni keys into one DCS and configures a provider on it.
// put and del ignore the test context: they also run from Cleanup.
type dcsBackend struct {
	name   string
	put    func(t *testing.T, key, value string)
	del    func(t *testing.T, key string) // the key and everything under it
	key    func(ns string, parts ...string) string
	config func(ns string) map[string]any
}

func backends() []dcsBackend {
	return []dcsBackend{
		{
			name: dcsEtcd3,
			put: func(t *testing.T, key, value string) {
				t.Helper()

				_, err := etcdWriter.Put(context.Background(), key, value)
				require.NoError(t, err)
			},
			del: func(t *testing.T, key string) {
				t.Helper()

				_, err := etcdWriter.Delete(context.Background(), key, clientv3.WithPrefix())
				require.NoError(t, err)
			},
			key: func(ns string, parts ...string) string {
				return "/" + strings.Join(append([]string{itNamespace, ns}, parts...), "/")
			},
			config: func(ns string) map[string]any {
				return map[string]any{
					"dcs":       dcsEtcd3,
					"namespace": "/" + itNamespace + "/" + ns,
					"etcd3": map[string]any{
						"endpoints": []any{etcdURL},
						"username":  etcdROUser,
						"password":  etcdROPassword,
					},
				}
			},
		},
		{
			name: dcsConsul,
			put: func(t *testing.T, key, value string) {
				t.Helper()

				status, err := consulDo(context.Background(), http.MethodPut, "/v1/kv/"+key, consulRootToken, value)
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, status)
			},
			del: func(t *testing.T, key string) {
				t.Helper()

				status, err := consulDo(context.Background(), http.MethodDelete, "/v1/kv/"+key+"?recurse=true",
					consulRootToken, nil)
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, status)
			},
			key: func(ns string, parts ...string) string {
				return strings.Join(append([]string{itNamespace, ns}, parts...), "/")
			},
			config: func(ns string) map[string]any {
				return map[string]any{
					"dcs":       dcsConsul,
					"namespace": itNamespace + "/" + ns,
					"consul": map[string]any{
						"address": consulURL,
						"token":   consulROToken,
					},
				}
			},
		},
	}
}

// eachBackend runs fn once per DCS, each in its own namespace.
func eachBackend(t *testing.T, fn func(t *testing.T, b dcsBackend, ns string)) {
	t.Helper()

	for _, b := range backends() {
		t.Run(b.name, func(t *testing.T) {
			ns := strings.ToLower(strings.NewReplacer("/", "-", "_", "-").Replace(t.Name()))
			t.Cleanup(func() { b.del(t, b.key(ns)) })

			fn(t, b, ns)
		})
	}
}

func memberJSON(host, port string) string {
	return fmt.Sprintf(`{"conn_url":"postgres://%s:%s/postgres","api_url":"http://%s:8008/patroni",`+
		`"state":"running","role":"replica","version":"4.0.0"}`, host, port, host)
}

func newITProvider(t *testing.T, b dcsBackend, ns string, extra map[string]any) *Provider {
	t.Helper()

	raw := b.config(ns)
	raw["user"] = "test"
	raw["password"] = "test"

	for k, v := range extra {
		raw[k] = v
	}

	p, err := NewProvider("it", raw, false, zap.NewNop())
	require.NoError(t, err)

	t.Cleanup(func() { _ = p.Close() })

	return p
}

func createDatabase(t *testing.T, name string) {
	t.Helper()

	quoted := pgx.Identifier{name}.Sanitize()

	_, err := tc.Admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = tc.Admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)")
	})
}

func dropDatabase(t *testing.T, name string) {
	t.Helper()

	_, err := tc.Admin.Exec(t.Context(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	require.NoError(t, err)
}

func TestIntegrationDiscoverScopes(t *testing.T) {
	createDatabase(t, "pit_scopes_app")
	createDatabase(t, "pit_scopes_billing")

	eachBackend(t, func(t *testing.T, b dcsBackend, ns string) {
		b.put(t, b.key(ns, "alpha", "members", "alpha-1"), memberJSON(tc.Host, tc.Port))
		b.put(t, b.key(ns, "alpha", "members", "alpha-2"), memberJSON(deadHost, "5432"))
		b.put(t, b.key(ns, "alpha", "leader"), "alpha-1")
		b.put(t, b.key(ns, "alpha", "config"), `{"ttl":30,"loop_wait":10}`)
		b.put(t, b.key(ns, "beta", "members", "beta-1"), memberJSON(tc.Host, tc.Port))
		// Leftovers of a removed cluster: no members, no cluster.
		b.put(t, b.key(ns, "gone", "config"), `{}`)
		b.put(t, b.key(ns, "gone", "history"), `[]`)
		b.put(t, b.key(ns, "test-scratch", "members", "t-1"), memberJSON(tc.Host, tc.Port))

		p := newITProvider(t, b, ns, map[string]any{
			"db":            "^pit_scopes_",
			"exclude_scope": "test-.*",
			"overrides": map[string]any{
				"beta": map[string]any{"db": "^pit_scopes_app$"},
			},
		})

		clusters, err := p.Discover(t.Context())
		require.NoError(t, err)

		got := byName(clusters)
		require.Equal(t, []string{"alpha", "beta"}, slices.Sorted(maps.Keys(got)))

		alpha := got["alpha"]
		assert.Equal(t, config.SourcePatroni, alpha.Source)
		assert.Equal(t, []string{tc.Host, deadHost}, hostNames(alpha))
		assert.Equal(t, []string{"pit_scopes_app", "pit_scopes_billing"}, dbNames(alpha))
		assert.Equal(t, "alpha", alpha.Labels[config.LabelPatroniScope])
		assert.Equal(t, b.name, alpha.Labels[config.LabelPatroniDCS])

		meta := alpha.HostMeta[config.Host(tc.Host)]
		assert.Equal(t, "alpha-1", meta.Alias)
		assert.Equal(t, tc.Port, meta.Port)

		assert.Equal(t, []string{"pit_scopes_app"}, dbNames(got["beta"]))
	})
}

func TestIntegrationDiscoverFollowsChanges(t *testing.T) {
	createDatabase(t, "pit_chg_app")

	eachBackend(t, func(t *testing.T, b dcsBackend, ns string) {
		p := newITProvider(t, b, ns, map[string]any{"db": "^pit_chg_"})

		discover := func() map[string]config.Cluster {
			t.Helper()

			clusters, err := p.Discover(t.Context())
			require.NoError(t, err)

			return byName(clusters)
		}

		// An empty namespace is a valid answer: no clusters.
		assert.Empty(t, discover())

		b.put(t, b.key(ns, "alpha", "members", "alpha-1"), memberJSON(tc.Host, tc.Port))

		got := discover()
		require.Contains(t, got, "alpha")
		assert.Equal(t, []string{tc.Host}, hostNames(got["alpha"]))
		assert.Equal(t, []string{"pit_chg_app"}, dbNames(got["alpha"]))

		// A replica joins and a database is created.
		createDatabase(t, "pit_chg_extra")
		b.put(t, b.key(ns, "alpha", "members", "alpha-2"), memberJSON(deadHost, "5432"))

		got = discover()
		assert.Equal(t, []string{tc.Host, deadHost}, hostNames(got["alpha"]))
		assert.Equal(t, []string{"pit_chg_app", "pit_chg_extra"}, dbNames(got["alpha"]))

		// The reachable member leaves: hosts follow the DCS, databases come from the cache.
		b.del(t, b.key(ns, "alpha", "members", "alpha-1"))
		dropDatabase(t, "pit_chg_extra")

		got = discover()
		assert.Equal(t, []string{deadHost}, hostNames(got["alpha"]))
		assert.Equal(t, []string{"pit_chg_app", "pit_chg_extra"}, dbNames(got["alpha"]))

		// The cluster is removed and another one appears.
		b.del(t, b.key(ns, "alpha"))
		b.put(t, b.key(ns, "epsilon", "members", "epsilon-1"), memberJSON(tc.Host, tc.Port))

		got = discover()
		assert.NotContains(t, got, "alpha")
		require.Contains(t, got, "epsilon")
		assert.Equal(t, []string{"pit_chg_app"}, dbNames(got["epsilon"]))
	})
}

func TestIntegrationStandbyLink(t *testing.T) {
	eachBackend(t, func(t *testing.T, b dcsBackend, ns string) {
		b.put(t, b.key(ns, "main", "members", "main-1"), memberJSON(tc.Host, tc.Port))
		b.put(t, b.key(ns, "main", "initialize"), "7412345678901234567")
		// The standby points at a VIP that no member has: linked by system identifier.
		b.put(t, b.key(ns, "dr", "members", "dr-1"), memberJSON(deadHost, "5432"))
		b.put(t, b.key(ns, "dr", "initialize"), "7412345678901234567")
		b.put(t, b.key(ns, "dr", "config"), `{"standby_cluster":{"host":"main-vip.example","port":5432}}`)
		// A standby that points straight at a member, without initialize.
		b.put(t, b.key(ns, "dr2", "members", "dr2-1"), memberJSON(deadHost, "6432"))
		b.put(t, b.key(ns, "dr2", "config"),
			fmt.Sprintf(`{"standby_cluster":{"host":%q,"port":%q}}`, tc.Host, tc.Port))

		p := newITProvider(t, b, ns, map[string]any{"db": "^pit_standby_none$"})

		clusters, err := p.Discover(t.Context())
		require.NoError(t, err)

		got := byName(clusters)
		assert.Equal(t, "main", got["dr"].Labels[config.LabelPatroniStandbyOf])
		assert.Equal(t, "main", got["dr2"].Labels[config.LabelPatroniStandbyOf])
		assert.NotContains(t, got["main"].Labels, config.LabelPatroniStandbyOf)
	})
}

func TestIntegrationDCSFailureKeepsState(t *testing.T) {
	createDatabase(t, "pit_fail_app")

	eachBackend(t, func(t *testing.T, b dcsBackend, ns string) {
		b.put(t, b.key(ns, "alpha", "members", "alpha-1"), memberJSON(tc.Host, tc.Port))

		p := newITProvider(t, b, ns, map[string]any{"db": "^pit_fail_"})

		_, err := p.Discover(t.Context())
		require.NoError(t, err)

		before := p.state

		// Point the provider at a DCS that does not answer.
		bad := p.cfg
		bad.Etcd3.Endpoints = []string{"http://127.0.0.1:1"}
		bad.Consul.Address = "http://127.0.0.1:1"

		down, err := newStore(bad, zap.NewNop())
		require.NoError(t, err)

		good := p.store
		p.store = down

		t.Cleanup(func() { _ = down.Close() })

		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		_, err = p.Discover(ctx)
		require.Error(t, err)
		assert.Equal(t, before, p.state, "a failed cycle must not touch the state")

		p.store = good
	})
}

func TestIntegrationReadOnlyCredentials(t *testing.T) {
	t.Run("etcd role cannot write", func(t *testing.T) {
		ro, err := clientv3.New(clientv3.Config{ //nolint:exhaustruct
			Endpoints:   []string{etcdURL},
			Username:    etcdROUser,
			Password:    etcdROPassword,
			DialTimeout: 5 * time.Second,
		})
		require.NoError(t, err)

		defer ro.Close()

		_, err = ro.Put(t.Context(), "/"+itNamespace+"/probe", "x")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})

	t.Run("consul token cannot write", func(t *testing.T) {
		status, err := consulDo(t.Context(), http.MethodPut, "/v1/kv/"+itNamespace+"/probe", consulROToken, "x")
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, status)
	})

	t.Run("etcd wrong password fails the cycle", func(t *testing.T) {
		b := backends()[0]
		raw := b.config("wrong-password")
		raw["etcd3"].(map[string]any)["password"] = "nope"
		raw["user"] = "test"

		p, err := NewProvider("it", raw, false, zap.NewNop())
		require.NoError(t, err)

		defer p.Close()

		_, err = p.Discover(t.Context())
		require.Error(t, err)
	})

	// Without a token Consul hides the keys from the list instead of refusing:
	// that must fail the cycle, not publish an empty namespace.
	t.Run("consul without token fails the cycle", func(t *testing.T) {
		b := backends()[1]
		b.put(t, b.key("no-token", "alpha", "members", "alpha-1"), memberJSON(tc.Host, tc.Port))
		t.Cleanup(func() { b.del(t, b.key("no-token")) })

		raw := b.config("no-token")
		delete(raw["consul"].(map[string]any), "token")
		raw["user"] = "test"

		p, err := NewProvider("it", raw, false, zap.NewNop())
		require.NoError(t, err)

		defer p.Close()

		_, err = p.Discover(t.Context())
		require.ErrorIs(t, err, errConsulForbidden)
	})
}
