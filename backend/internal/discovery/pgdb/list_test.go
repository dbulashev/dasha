package pgdb

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestDSNEscapesCredentials(t *testing.T) {
	t.Parallel()

	target := Target{
		Host:     "pg-01.local",
		Port:     "5433",
		User:     "dasha",
		Password: "p@ss:/word?",
		Database: "admin_db",
	}

	connCfg, err := pgx.ParseConfig(target.dsn())
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	if connCfg.User != "dasha" || connCfg.Password != "p@ss:/word?" {
		t.Errorf("credentials = %q / %q, want dasha / p@ss:/word?", connCfg.User, connCfg.Password)
	}

	if connCfg.Host != "pg-01.local" || connCfg.Port != 5433 {
		t.Errorf("address = %s:%d, want pg-01.local:5433", connCfg.Host, connCfg.Port)
	}

	if connCfg.Database != "admin_db" {
		t.Errorf("Database = %q, want admin_db", connCfg.Database)
	}
}

func TestDSNBracketsIPv6(t *testing.T) {
	t.Parallel()

	connCfg, err := pgx.ParseConfig(Target{Host: "fd00::1", Port: "5432", User: "u", Database: "postgres"}.dsn()) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	if connCfg.Host != "fd00::1" {
		t.Errorf("Host = %q, want fd00::1", connCfg.Host)
	}
}

var errDown = errors.New("down")

// fakeLister answers for the hosts in up and records the order it was asked in.
type fakeLister struct {
	up    map[string][]string
	asked []string
}

func (f *fakeLister) list(_ context.Context, t Target) ([]string, error) {
	f.asked = append(f.asked, t.Host)

	if names, ok := f.up[t.Host]; ok {
		return names, nil
	}

	return nil, errDown
}

func targets(hosts ...string) []Target {
	out := make([]Target, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, Target{Host: h}) //nolint:exhaustruct
	}

	return out
}

func TestListFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		up        []string
		start     int
		wantIdx   int
		wantAsked []string
	}{
		{name: "first answers", up: []string{"a", "b", "c"}, start: 0, wantIdx: 0, wantAsked: []string{"a"}},
		{name: "falls through", up: []string{"c"}, start: 0, wantIdx: 2, wantAsked: []string{"a", "b", "c"}},
		{name: "starts at sticky", up: []string{"a", "b", "c"}, start: 1, wantIdx: 1, wantAsked: []string{"b"}},
		{name: "wraps around", up: []string{"a"}, start: 1, wantIdx: 0, wantAsked: []string{"b", "c", "a"}},
		{name: "start out of range", up: []string{"a"}, start: 7, wantIdx: 0, wantAsked: []string{"a"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := &fakeLister{up: map[string][]string{}, asked: nil}
			for _, h := range tt.up {
				f.up[h] = []string{"app"}
			}

			names, idx, err := ListFirst(t.Context(), f.list, targets("a", "b", "c"), tt.start)
			if err != nil {
				t.Fatalf("ListFirst() error = %v", err)
			}

			if idx != tt.wantIdx || !slices.Equal(names, []string{"app"}) {
				t.Errorf("ListFirst() = %v, %d, want [app], %d", names, idx, tt.wantIdx)
			}

			if !slices.Equal(f.asked, tt.wantAsked) {
				t.Errorf("asked %v, want %v", f.asked, tt.wantAsked)
			}
		})
	}
}

func TestListFirstAllFail(t *testing.T) {
	t.Parallel()

	f := &fakeLister{up: nil, asked: nil}

	_, idx, err := ListFirst(t.Context(), f.list, targets("a", "b"), 1)
	if !errors.Is(err, errDown) {
		t.Fatalf("ListFirst() error = %v, want %v", err, errDown)
	}

	if !strings.HasPrefix(err.Error(), "a: ") {
		t.Errorf("error = %q, want it to name the last host tried", err)
	}

	if idx != 1 {
		t.Errorf("idx = %d, want the start index kept", idx)
	}
}

func TestListFirstNoTargets(t *testing.T) {
	t.Parallel()

	if _, _, err := ListFirst(t.Context(), (&fakeLister{}).list, nil, 0); !errors.Is(err, errNoTargets) { //nolint:exhaustruct
		t.Fatalf("ListFirst() error = %v, want %v", err, errNoTargets)
	}
}

func TestListFirstStopsOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	f := &fakeLister{up: nil, asked: nil}

	if _, _, err := ListFirst(ctx, f.list, targets("a", "b", "c"), 0); err == nil {
		t.Fatal("ListFirst() error = nil, want one")
	}

	if len(f.asked) != 1 {
		t.Errorf("asked %v, want a single attempt after cancel", f.asked)
	}
}
