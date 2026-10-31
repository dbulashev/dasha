package patroni

import (
	"maps"
	"testing"

	"github.com/dbulashev/dasha/internal/config"
)

func primaryScope(name, sysid string, hosts ...string) scopeData {
	s := scopeData{Name: name, SysID: sysid} //nolint:exhaustruct
	for i, h := range hosts {
		s.Members = append(s.Members, member{Name: name + "-" + string(rune('1'+i)), Host: h, Port: "5432"})
	}

	return s
}

func standbyScope(name, sysid string, targets ...hostPort) scopeData {
	s := primaryScope(name, sysid, "10.9.9."+name)
	s.IsStandby = true
	s.Standby = targets

	return s
}

func clusterNames(scopes []scopeData) map[string]config.ClusterName {
	out := make(map[string]config.ClusterName, len(scopes))
	for _, s := range scopes {
		out[s.Name] = config.ClusterName("e_" + s.Name)
	}

	return out
}

func TestStandbyOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		scopes []scopeData
		want   map[string]config.ClusterName
	}{
		{
			name: "sysid, standby points at a VIP",
			scopes: []scopeData{
				primaryScope("beta", "42", "10.0.0.1"),
				standbyScope("beta-standby", "42", hostPort{"beta-vip", "5432"}),
			},
			want: map[string]config.ClusterName{"beta-standby": "e_beta"},
		},
		{
			name: "two primaries with the sysid fall back to the address",
			scopes: []scopeData{
				primaryScope("beta", "42", "10.0.0.1"),
				primaryScope("beta-clone", "42", "10.0.0.2"),
				standbyScope("beta-standby", "42", hostPort{"10.0.0.2", "5432"}),
			},
			want: map[string]config.ClusterName{"beta-standby": "e_beta-clone"},
		},
		{
			name: "ambiguous sysid and a VIP give no link",
			scopes: []scopeData{
				primaryScope("beta", "42", "10.0.0.1"),
				primaryScope("beta-clone", "42", "10.0.0.2"),
				standbyScope("beta-standby", "42", hostPort{"beta-vip", "5432"}),
			},
			want: map[string]config.ClusterName{},
		},
		{
			name: "no initialize, address with a host list, case-insensitive",
			scopes: []scopeData{
				primaryScope("alpha", "", "PG-1.local", "pg-2.local"),
				standbyScope("alpha-dr", "", hostPort{"nowhere", "5432"}, hostPort{"pg-1.LOCAL", "5432"}),
			},
			want: map[string]config.ClusterName{"alpha-dr": "e_alpha"},
		},
		{
			name: "port must match",
			scopes: []scopeData{
				primaryScope("alpha", "", "10.0.0.1"),
				standbyScope("alpha-dr", "", hostPort{"10.0.0.1", "5433"}),
			},
			want: map[string]config.ClusterName{},
		},
		{
			name: "primary filtered out",
			scopes: []scopeData{
				standbyScope("beta-standby", "42", hostPort{"10.0.0.1", "5432"}),
			},
			want: map[string]config.ClusterName{},
		},
		{
			name: "cascade links to the root primary by sysid",
			scopes: []scopeData{
				primaryScope("beta", "42", "10.0.0.1"),
				standbyScope("beta-dr1", "42", hostPort{"10.0.0.1", "5432"}),
				standbyScope("beta-dr2", "42", hostPort{"10.9.9.beta-dr1", "5432"}),
			},
			want: map[string]config.ClusterName{"beta-dr1": "e_beta", "beta-dr2": "e_beta"},
		},
		{
			name: "self reference is ignored",
			scopes: []scopeData{
				standbyScope("solo", "", hostPort{"10.9.9.solo", "5432"}),
			},
			want: map[string]config.ClusterName{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := standbyOf(tt.scopes, clusterNames(tt.scopes)); !maps.Equal(got, tt.want) {
				t.Errorf("standbyOf() = %v, want %v", got, tt.want)
			}
		})
	}
}
