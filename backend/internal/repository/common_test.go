package repository

import (
	"testing"

	"github.com/dbulashev/dasha/internal/config"
)

func TestHostPort(t *testing.T) {
	t.Parallel()

	meta := map[config.Host]config.HostMeta{
		"10.0.0.1": {Port: "5433", Alias: "pg-1"},
		"10.0.0.2": {Port: "", Alias: "pg-2"},
	}

	tests := []struct {
		name string
		cl   config.Cluster
		host config.Host
		want string
	}{
		{name: "host meta wins", cl: config.Cluster{Port: "6432", HostMeta: meta}, host: "10.0.0.1", want: "5433"},
		{name: "meta without port", cl: config.Cluster{Port: "6432", HostMeta: meta}, host: "10.0.0.2", want: "6432"},
		{name: "host not in meta", cl: config.Cluster{Port: "6432", HostMeta: meta}, host: "10.0.0.3", want: "6432"},
		{name: "cluster port", cl: config.Cluster{Port: "6432"}, host: "pg-01", want: "6432"},
		{name: "default", cl: config.Cluster{}, host: "pg-01", want: "5432"},
		{name: "meta port, no cluster port", cl: config.Cluster{HostMeta: meta}, host: "10.0.0.1", want: "5433"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := hostPort(tt.cl, tt.host); got != tt.want {
				t.Errorf("hostPort() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPoolDSN(t *testing.T) {
	t.Parallel()

	cl := config.Cluster{ //nolint:exhaustruct
		UserName: "dasha",
		Password: "p@ss:word",
		Port:     "6432",
		HostMeta: map[config.Host]config.HostMeta{
			"10.0.0.1:5433": {Address: "10.0.0.1", Port: "5433", Alias: "pg-1"},
		},
	}

	tests := []struct {
		name string
		cl   config.Cluster
		host config.Host
		want string
	}{
		{name: "address from meta", cl: cl, host: "10.0.0.1:5433", want: "postgres://dasha:p%40ss%3Aword@10.0.0.1:5433/app"},
		{name: "host as address", cl: cl, host: "pg-01", want: "postgres://dasha:p%40ss%3Aword@pg-01:6432/app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := poolDSN(tt.cl, tt.host, "app"); got != tt.want {
				t.Errorf("poolDSN() = %q, want %q", got, tt.want)
			}
		})
	}
}
