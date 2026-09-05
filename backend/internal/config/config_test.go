package config

import (
	"strings"
	"testing"
)

func TestLogSearchValidate(t *testing.T) {
	t.Parallel()

	source := func(mutate func(*LogSourceConfig)) LogSourceConfig {
		s := LogSourceConfig{
			Type:      LogSourceTypeOpenSearch,
			Addresses: []string{"https://os:9200"},
			Auth:      LogSourceAuthConfig{Kind: LogAuthBasic, User: "dasha", Password: "s3cret"},
			Streams: map[string]LogStreamConfig{
				"postgresql": {Index: "pg-*"},
			},
		}

		if mutate != nil {
			mutate(&s)
		}

		return s
	}

	tests := []struct {
		name     string
		cfg      LogSearchConfig
		clusters []Cluster
		wantErr  string
	}{
		{
			name: "valid",
			cfg: LogSearchConfig{
				DefaultSource: "main",
				Sources:       map[string]LogSourceConfig{"main": source(nil)},
			},
		},
		{
			name: "default source is not defined",
			cfg: LogSearchConfig{
				DefaultSource: "other",
				Sources:       map[string]LogSourceConfig{"main": source(nil)},
			},
			wantErr: "default_source",
		},
		{
			name: "unknown type",
			cfg: LogSearchConfig{
				Sources: map[string]LogSourceConfig{
					"main": source(func(s *LogSourceConfig) { s.Type = "loki" }),
				},
			},
			wantErr: "unknown type",
		},
		{
			name: "no addresses",
			cfg: LogSearchConfig{
				Sources: map[string]LogSourceConfig{
					"main": source(func(s *LogSourceConfig) { s.Addresses = nil }),
				},
			},
			wantErr: "addresses",
		},
		{
			name: "no streams",
			cfg: LogSearchConfig{
				Sources: map[string]LogSourceConfig{
					"main": source(func(s *LogSourceConfig) { s.Streams = nil }),
				},
			},
			wantErr: "stream",
		},
		{
			name: "stream without an index",
			cfg: LogSearchConfig{
				Sources: map[string]LogSourceConfig{
					"main": source(func(s *LogSourceConfig) {
						s.Streams = map[string]LogStreamConfig{"postgresql": {}}
					}),
				},
			},
			wantErr: "index",
		},
		{
			name: "basic auth without a user",
			cfg: LogSearchConfig{
				Sources: map[string]LogSourceConfig{
					"main": source(func(s *LogSourceConfig) { s.Auth.User = "" }),
				},
			},
			wantErr: "auth.user",
		},
		{
			name: "basic auth whose secret never arrived",
			cfg: LogSearchConfig{
				Sources: map[string]LogSourceConfig{
					"main": source(func(s *LogSourceConfig) {
						s.Auth.Password = ""
						s.Auth.PasswordFromEnv = "OS_PASSWORD"
					}),
				},
			},
			wantErr: "OS_PASSWORD",
		},
		{
			name: "api key auth without a key",
			cfg: LogSearchConfig{
				Sources: map[string]LogSourceConfig{
					"main": source(func(s *LogSourceConfig) {
						s.Auth = LogSourceAuthConfig{Kind: LogAuthAPIKey}
					}),
				},
			},
			wantErr: "api_key",
		},
		{
			name: "stream the API does not serve",
			cfg: LogSearchConfig{
				Sources: map[string]LogSourceConfig{
					"main": source(func(s *LogSourceConfig) {
						s.Streams = map[string]LogStreamConfig{"pg": {Index: "pg-*"}}
					}),
				},
			},
			wantErr: "unknown stream",
		},
		{
			name: "cluster names a source that does not exist",
			cfg: LogSearchConfig{
				Sources: map[string]LogSourceConfig{"main": source(nil)},
			},
			clusters: []Cluster{{Name: "prod", LogSource: "mian"}},
			wantErr:  "log_source",
		},
		{
			name: "cluster pinned to the built-in source",
			cfg: LogSearchConfig{
				Sources: map[string]LogSourceConfig{"main": source(nil)},
			},
			clusters: []Cluster{{Name: "prod", LogSource: SourceYandexMDB}},
		},
		{
			name: "unknown auth kind",
			cfg: LogSearchConfig{
				Sources: map[string]LogSourceConfig{
					"main": source(func(s *LogSourceConfig) { s.Auth.Kind = "kerberos" }),
				},
			},
			wantErr: "auth.kind",
		},
		{
			name: "reserved source name",
			cfg: LogSearchConfig{
				Sources: map[string]LogSourceConfig{SourceYandexMDB: source(nil)},
			},
			wantErr: "reserved",
		},
		{
			name: "credentials over plain http",
			cfg: LogSearchConfig{
				Sources: map[string]LogSourceConfig{
					"main": source(func(s *LogSourceConfig) { s.Addresses = []string{"http://os:9200"} }),
				},
			},
			wantErr: "https",
		},
		{
			name: "plain http without credentials",
			cfg: LogSearchConfig{
				Sources: map[string]LogSourceConfig{
					"main": source(func(s *LogSourceConfig) {
						s.Addresses = []string{"http://os:9200"}
						s.Auth = LogSourceAuthConfig{Kind: LogAuthNone}
					}),
				},
			},
		},
		{
			name: "no sources at all",
			cfg:  LogSearchConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate(tt.clusters)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}

			if err == nil {
				t.Fatalf("expected an error mentioning %q", tt.wantErr)
			}

			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestLogSearchWithDefaults(t *testing.T) {
	t.Parallel()

	cfg := LogSearchConfig{
		Sources: map[string]LogSourceConfig{
			"main": {Type: LogSourceTypeOpenSearch},
		},
	}.WithDefaults()

	if cfg.MaxScan != DefaultLogSearchMaxScan || cfg.MaxPageSize != DefaultLogSearchMaxPageSize {
		t.Errorf("global defaults not applied: %+v", cfg)
	}

	src := cfg.Sources["main"]
	if src.BatchSize != DefaultLogSourceBatchSize || src.MaxBoundaryIDs != DefaultLogSourceMaxBoundaryIDs {
		t.Errorf("source defaults not applied: %+v", src)
	}

	if src.Auth.Kind != LogAuthNone {
		t.Errorf("auth kind = %q, want %q", src.Auth.Kind, LogAuthNone)
	}

	// A source without limits of its own inherits the global values, but not the
	// pointers: one source's limits must not change another's.
	if *src.RateLimit != *cfg.RateLimit || *src.AdminRateLimit != *cfg.AdminRateLimit {
		t.Error("source did not inherit the global rate limits")
	}

	if src.RateLimit == cfg.RateLimit || src.AdminRateLimit == cfg.AdminRateLimit {
		t.Error("source shares the global rate limit values")
	}
}

func TestLogSearchWithDefaultsLeavesTheInputAlone(t *testing.T) {
	t.Parallel()

	in := LogSearchConfig{
		Sources: map[string]LogSourceConfig{
			"main": {Type: LogSourceTypeOpenSearch},
		},
	}

	_ = in.WithDefaults()

	if got := in.Sources["main"]; got.BatchSize != 0 || got.RateLimit != nil {
		t.Errorf("WithDefaults wrote back into the caller's sources: %+v", got)
	}

	if in.MaxScan != 0 || in.RateLimit != nil {
		t.Errorf("WithDefaults mutated the receiver: %+v", in)
	}
}
