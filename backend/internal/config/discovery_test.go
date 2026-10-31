package config

import (
	"slices"
	"testing"
	"time"
)

// probeConfig stands in for a provider config: it covers the decoder settings
// (weak typing, slice and duration hooks) without tying the test to one type.
type probeConfig struct {
	Port  string        `mapstructure:"port"`
	Hosts []string      `mapstructure:"hosts"`
	TTL   time.Duration `mapstructure:"ttl"`
}

func TestDecodeDiscoveryConfigYandexMDB(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"authorized_key":    "/etc/dasha/key.json",
		"folder_id":         "b1g123",
		"user":              "monitoring",
		"password_from_env": "DASHA_PG_PASSWORD",
		"refresh_interval":  "7", // viper hands strings over unchanged
		"clusters": []any{
			map[string]any{
				"name":       "prod-.*",
				"exclude_db": "template.*",
			},
		},
		"unknown_key": "ignored",
	}

	cfg, unused, err := DecodeDiscoveryConfig[YandexMDBConfig](raw)
	if err != nil {
		t.Fatalf("DecodeDiscoveryConfig() error = %v", err)
	}

	if want := []string{"unknown_key"}; !slices.Equal(unused, want) {
		t.Errorf("unused = %v, want %v", unused, want)
	}

	if cfg.AuthorizedKey != "/etc/dasha/key.json" || cfg.FolderID != "b1g123" || cfg.User != "monitoring" {
		t.Fatalf("scalar fields decoded as %+v", cfg)
	}

	if cfg.RefreshInterval != 7*time.Minute {
		t.Errorf("RefreshInterval = %v, want 7m", cfg.RefreshInterval)
	}

	if len(cfg.Clusters) != 1 {
		t.Fatalf("Clusters = %+v, want one filter", cfg.Clusters)
	}

	f := cfg.Clusters[0]
	if f.Name != "prod-.*" {
		t.Errorf("filter name = %q, want prod-.*", f.Name)
	}

	if f.Db != nil {
		t.Errorf("Db = %v, want nil for an unset key", *f.Db)
	}

	if f.ExcludeDb == nil || *f.ExcludeDb != "template.*" {
		t.Errorf("ExcludeDb = %v, want template.*", f.ExcludeDb)
	}
}

func TestDecodeDiscoveryConfigWeakTyping(t *testing.T) {
	t.Parallel()

	// A yaml number into a string field, a comma-separated list into a slice and
	// a duration string — all three come from viper's decoder settings.
	raw := map[string]any{
		"port":  5432,
		"hosts": "a,b",
		"ttl":   "30s",
	}

	cfg, unused, err := DecodeDiscoveryConfig[probeConfig](raw)
	if err != nil {
		t.Fatalf("DecodeDiscoveryConfig() error = %v", err)
	}

	if len(unused) != 0 {
		t.Errorf("unused = %v, want none", unused)
	}

	if cfg.Port != "5432" {
		t.Errorf("Port = %q, want 5432", cfg.Port)
	}

	if want := []string{"a", "b"}; !slices.Equal(cfg.Hosts, want) {
		t.Errorf("Hosts = %v, want %v", cfg.Hosts, want)
	}

	if cfg.TTL != 30*time.Second {
		t.Errorf("TTL = %v, want 30s", cfg.TTL)
	}
}

func TestDecodeDiscoveryConfigEmpty(t *testing.T) {
	t.Parallel()

	cfg, _, err := DecodeDiscoveryConfig[YandexMDBConfig](nil)
	if err != nil {
		t.Fatalf("DecodeDiscoveryConfig() error = %v", err)
	}

	if cfg.FolderID != "" || len(cfg.Clusters) != 0 {
		t.Errorf("got %+v, want zero value", cfg)
	}
}

func TestDecodeDiscoveryConfigTypeMismatch(t *testing.T) {
	t.Parallel()

	if _, _, err := DecodeDiscoveryConfig[YandexMDBConfig](map[string]any{
		"refresh_interval": "not-a-number",
	}); err == nil {
		t.Fatal("DecodeDiscoveryConfig() error = nil, want a decode error")
	}
}

func TestDecodeDiscoveryConfigReportsTypos(t *testing.T) {
	t.Parallel()

	// A misspelled key is not an error — an old config keeps working — but it is
	// reported, because "excludedb" silently widens what gets monitored.
	cfg, unused, err := DecodeDiscoveryConfig[probeConfig](map[string]any{
		"port":      "5432",
		"excludedb": "template.*",
		"hostz":     []any{"pg-01"},
	})
	if err != nil {
		t.Fatalf("DecodeDiscoveryConfig() error = %v", err)
	}

	if want := []string{"excludedb", "hostz"}; !slices.Equal(unused, want) {
		t.Errorf("unused = %v, want %v (sorted)", unused, want)
	}

	if cfg.Port != "5432" {
		t.Errorf("Port = %q, want 5432: known keys must still decode", cfg.Port)
	}
}

func TestDecodeDiscoveryConfigRefreshInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  any
		want time.Duration
	}{
		{name: "int is minutes", raw: 5, want: 5 * time.Minute},
		{name: "numeric string is minutes", raw: "5", want: 5 * time.Minute},
		{name: "float is minutes", raw: 1.5, want: 90 * time.Second},
		{name: "duration string", raw: "5m", want: 5 * time.Minute},
		{name: "seconds string", raw: "10s", want: 10 * time.Second},
		{name: "negative", raw: -1, want: -time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, _, err := DecodeDiscoveryConfig[YandexMDBConfig](map[string]any{"refresh_interval": tt.raw})
			if err != nil {
				t.Fatalf("DecodeDiscoveryConfig() error = %v", err)
			}

			if cfg.RefreshInterval != tt.want {
				t.Errorf("RefreshInterval = %v, want %v", cfg.RefreshInterval, tt.want)
			}
		})
	}
}

func TestDecodeDiscoveryConfigRefreshIntervalOverflow(t *testing.T) {
	t.Parallel()

	if _, _, err := DecodeDiscoveryConfig[YandexMDBConfig](map[string]any{
		"refresh_interval": 1e300,
	}); err == nil {
		t.Fatal("DecodeDiscoveryConfig() error = nil, want an out-of-range error")
	}
}

func TestDiscoveryInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in         time.Duration
		want       time.Duration
		wantRaised bool
	}{
		{in: 0, want: DefaultDiscoveryInterval},
		{in: -time.Minute, want: DefaultDiscoveryInterval},
		{in: 10 * time.Second, want: MinDiscoveryInterval, wantRaised: true},
		{in: time.Minute, want: time.Minute},
		{in: 7 * time.Minute, want: 7 * time.Minute},
	}

	for _, tt := range tests {
		got, raised := DiscoveryInterval(tt.in)
		if got != tt.want || raised != tt.wantRaised {
			t.Errorf("DiscoveryInterval(%v) = %v, %v, want %v, %v", tt.in, got, raised, tt.want, tt.wantRaised)
		}
	}
}
