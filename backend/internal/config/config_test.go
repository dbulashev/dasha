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
			Auth:      LogSourceAuthConfig{Kind: LogAuthBasic},
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
		name    string
		cfg     LogSearchConfig
		wantErr string
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
			name: "unknown auth kind",
			cfg: LogSearchConfig{
				Sources: map[string]LogSourceConfig{
					"main": source(func(s *LogSourceConfig) { s.Auth.Kind = "kerberos" }),
				},
			},
			wantErr: "auth.kind",
		},
		{
			name: "no sources at all",
			cfg:  LogSearchConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate()

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

	// A source without limits of its own inherits the global ones.
	if src.RateLimit != cfg.RateLimit || src.AdminRateLimit != cfg.AdminRateLimit {
		t.Error("source did not inherit the global rate limits")
	}
}
