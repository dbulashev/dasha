package postgres

import (
	"errors"
	"testing"
	"time"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     Config
		wantErr error
	}{
		{
			name:    "no hosts",
			cfg:     Config{User: "dasha"}, //nolint:exhaustruct
			wantErr: errHostsRequired,
		},
		{
			name:    "empty host entry",
			cfg:     Config{Hosts: []string{"pg-01", ""}, User: "dasha"}, //nolint:exhaustruct
			wantErr: errEmptyHost,
		},
		{
			name:    "no user",
			cfg:     Config{Hosts: []string{"pg-01"}}, //nolint:exhaustruct
			wantErr: errUserRequired,
		},
		{
			name:    "complete",
			cfg:     Config{Hosts: []string{"pg-01"}, User: "dasha"}, //nolint:exhaustruct
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := tt.cfg.validate(); !errors.Is(err, tt.wantErr) {
				t.Fatalf("validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigWithDefaults(t *testing.T) {
	t.Parallel()

	cfg := Config{Hosts: []string{"pg-01"}, User: "dasha"}.withDefaults() //nolint:exhaustruct

	if cfg.Port != defaultPort {
		t.Errorf("Port = %q, want %q", cfg.Port, defaultPort)
	}

	if cfg.BootstrapDB != defaultBootstrapDB {
		t.Errorf("BootstrapDB = %q, want %q", cfg.BootstrapDB, defaultBootstrapDB)
	}
}

func TestConfigPasswordFromEnv(t *testing.T) {
	t.Setenv("DASHA_TEST_PG_PASSWORD", "s3cret")

	cfg := Config{ //nolint:exhaustruct
		Password:        "ignored",
		PasswordFromEnv: "DASHA_TEST_PG_PASSWORD",
	}.withDefaults()

	if cfg.Password != "s3cret" {
		t.Errorf("Password = %q, want the value from the environment", cfg.Password)
	}
}

func TestConfigInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		minutes int
		want    time.Duration
	}{
		{name: "explicit", minutes: 7, want: 7 * time.Minute},
		{name: "unset", minutes: 0, want: defaultRefreshInterval},
		{name: "negative", minutes: -3, want: defaultRefreshInterval},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := Config{RefreshInterval: tt.minutes} //nolint:exhaustruct
			if got := cfg.interval(); got != tt.want {
				t.Errorf("interval() = %v, want %v", got, tt.want)
			}
		})
	}
}
