package patroni

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/dbulashev/dasha/internal/config"
)

func etcdRaw(extra map[string]any) map[string]any {
	raw := map[string]any{
		"dcs":      "etcd3",
		"etcd3":    map[string]any{"endpoints": []any{"http://etcd:2379"}},
		"user":     "dasha",
		"password": "pw",
	}
	maps.Copy(raw, extra)

	return raw
}

func TestNewProviderValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     map[string]any
		wantErr error
	}{
		{name: "no dcs", raw: map[string]any{"user": "dasha"}, wantErr: errDCSRequired},
		{name: "unknown dcs", raw: etcdRaw(map[string]any{"dcs": "zookeeper"}), wantErr: errDCSUnknown},
		{name: "no endpoints", raw: etcdRaw(map[string]any{"etcd3": map[string]any{}}), wantErr: errEndpointsRequired},
		{
			name:    "empty endpoint",
			raw:     etcdRaw(map[string]any{"etcd3": map[string]any{"endpoints": []any{"http://a:2379", " "}}}),
			wantErr: errEmptyEndpoint,
		},
		{name: "no consul address", raw: etcdRaw(map[string]any{"dcs": "consul"}), wantErr: errAddressRequired},
		{name: "no user", raw: etcdRaw(map[string]any{"user": ""}), wantErr: errUserRequired},
		{
			name:    "override clears user",
			raw:     etcdRaw(map[string]any{"overrides": map[string]any{"beta": map[string]any{"user": ""}}}),
			wantErr: errUserRequired,
		},
		{name: "bad port", raw: etcdRaw(map[string]any{"port": "54 32"}), wantErr: errBadPort},
		{
			name:    "bad override port",
			raw:     etcdRaw(map[string]any{"overrides": map[string]any{"beta": map[string]any{"port": "abc"}}}),
			wantErr: errBadPort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := newProvider("e", tt.raw, false, zap.NewNop()); !errors.Is(err, tt.wantErr) {
				t.Fatalf("newProvider() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewProviderInvalidRegexps(t *testing.T) {
	t.Parallel()

	for _, raw := range []map[string]any{
		etcdRaw(map[string]any{"scope": "["}),
		etcdRaw(map[string]any{"exclude_scope": "["}),
		etcdRaw(map[string]any{"db": "["}),
		etcdRaw(map[string]any{"overrides": map[string]any{"beta": map[string]any{"exclude_db": "["}}}),
	} {
		if _, err := newProvider("e", raw, false, zap.NewNop()); err == nil {
			t.Errorf("newProvider(%v) error = nil, want a compile error", raw)
		}
	}
}

func TestNewProviderDefaults(t *testing.T) {
	t.Parallel()

	p, err := newProvider("e", etcdRaw(nil), false, zap.NewNop())
	if err != nil {
		t.Fatalf("newProvider() error = %v", err)
	}

	if p.namespace != "/service" || p.prefix != "/service/" {
		t.Errorf("namespace/prefix = %q/%q, want /service and /service/", p.namespace, p.prefix)
	}

	if p.Interval() != config.DefaultDiscoveryInterval {
		t.Errorf("Interval() = %v, want %v", p.Interval(), config.DefaultDiscoveryInterval)
	}

	if p.base.BootstrapDB != defaultPGDB || p.base.Port != "" {
		t.Errorf("base = %+v, want bootstrap postgres and no port", p.base)
	}

	if !p.scopes.MatchName("anything") {
		t.Error("default scope filter should match every scope")
	}
}

func TestNewProviderInterval(t *testing.T) {
	t.Parallel()

	p, err := newProvider("e", etcdRaw(map[string]any{"refresh_interval": 2}), false, zap.NewNop())
	if err != nil {
		t.Fatalf("newProvider() error = %v", err)
	}

	if p.Interval() != 2*time.Minute {
		t.Errorf("Interval() = %v, want 2m", p.Interval())
	}
}

func TestNewProviderWarnsAboutOtherDCSBlock(t *testing.T) {
	t.Parallel()

	core, recorded := observer.New(zapcore.WarnLevel)

	_, err := newProvider("e", etcdRaw(map[string]any{
		"consul": map[string]any{"address": "http://consul:8500"},
	}), false, zap.New(core))
	if err != nil {
		t.Fatalf("newProvider() error = %v", err)
	}

	if recorded.FilterMessage("settings of another dcs, ignored").Len() != 1 {
		t.Errorf("logs = %v, want one warning about the consul block", recorded.All())
	}
}

func TestOverridesMerge(t *testing.T) {
	t.Setenv("DASHA_TEST_PATRONI_ENTRY_PW", "from-env")
	t.Setenv("DASHA_TEST_PATRONI_BETA_PW", "beta-env")

	p, err := newProvider("e", map[string]any{
		"dcs":               "etcd3",
		"etcd3":             map[string]any{"endpoints": "http://etcd:2379"},
		"user":              "dasha",
		"password_from_env": "DASHA_TEST_PATRONI_ENTRY_PW",
		"port":              6432,
		"db":                "^app",
		"overrides": map[string]any{
			"beta":  map[string]any{"user": "dasha_beta", "password_from_env": "DASHA_TEST_PATRONI_BETA_PW"},
			"gamma": map[string]any{"password": "plain", "port": ""},
			"delta": map[string]any{"exclude_db": "_old$"},
			"Mixed": map[string]any{"bootstrap_db": "admin"},
		},
	}, false, zap.NewNop())
	if err != nil {
		t.Fatalf("newProvider() error = %v", err)
	}

	tests := []struct {
		scope, user, password, port, bootstrap string
	}{
		{scope: "alpha", user: "dasha", password: "from-env", port: "6432", bootstrap: "postgres"},
		{scope: "beta", user: "dasha_beta", password: "beta-env", port: "6432", bootstrap: "postgres"},
		{scope: "gamma", user: "dasha", password: "plain", port: "", bootstrap: "postgres"},
		{scope: "MIXED", user: "dasha", password: "from-env", port: "6432", bootstrap: "admin"},
	}

	for _, tt := range tests {
		pg := p.pgFor(tt.scope)
		if pg.User != tt.user || pg.Password != tt.password || pg.Port != tt.port || pg.BootstrapDB != tt.bootstrap {
			t.Errorf("pgFor(%q) = %s/%s/%s/%s, want %s/%s/%s/%s", tt.scope,
				pg.User, pg.Password, pg.Port, pg.BootstrapDB, tt.user, tt.password, tt.port, tt.bootstrap)
		}
	}

	delta := p.pgFor("delta")
	if !delta.filter.MatchDb("app") || delta.filter.MatchDb("app_old") || delta.filter.MatchDb("billing") {
		t.Error("delta: db from the entry and exclude_db from the override should both apply")
	}
}

func TestTLSBuild(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	junk := filepath.Join(dir, "junk.pem")

	if err := os.WriteFile(junk, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		cfg     TLSConfig
		wantErr string
	}{
		{name: "empty", cfg: TLSConfig{}}, //nolint:exhaustruct
		{name: "missing ca", cfg: TLSConfig{CAFile: filepath.Join(dir, "nope.pem")}, wantErr: "read tls.ca_file"}, //nolint:exhaustruct
		{name: "ca without certs", cfg: TLSConfig{CAFile: junk}, wantErr: "no certificates"},                      //nolint:exhaustruct
		{name: "cert without key", cfg: TLSConfig{CertFile: junk}, wantErr: "must be set together"},               //nolint:exhaustruct
	}

	for _, tt := range tests {
		cfg, err := tt.cfg.build()

		switch {
		case tt.wantErr == "" && (err != nil || cfg.MinVersion == 0):
			t.Errorf("%s: build() = %v, %v, want a config", tt.name, cfg, err)
		case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
			t.Errorf("%s: build() error = %v, want %q", tt.name, err, tt.wantErr)
		}
	}
}
