package patroni

import (
	"errors"
	"slices"
	"testing"
)

func TestNamespacePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ns, etcd, consul, label string
	}{
		{ns: "/service", etcd: "/service/", consul: "service/", label: "/service"},
		{ns: "/service/", etcd: "/service/", consul: "service/", label: "/service"},
		{ns: "service", etcd: "/service/", consul: "service/", label: "/service"},
		{ns: "//a//b/", etcd: "/a/b/", consul: "a/b/", label: "/a/b"},
		{ns: "/", etcd: "/", consul: "", label: "/"},
	}

	for _, tt := range tests {
		if got := namespacePrefix(tt.ns, dcsEtcd3); got != tt.etcd {
			t.Errorf("namespacePrefix(%q, etcd3) = %q, want %q", tt.ns, got, tt.etcd)
		}

		if got := namespacePrefix(tt.ns, dcsConsul); got != tt.consul {
			t.Errorf("namespacePrefix(%q, consul) = %q, want %q", tt.ns, got, tt.consul)
		}

		if got := normalizeNamespace(tt.ns); got != tt.label {
			t.Errorf("normalizeNamespace(%q) = %q, want %q", tt.ns, got, tt.label)
		}
	}
}

func TestMemberAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		host    string
		port    string
		wantErr error
	}{
		{name: "ipv4", value: `{"conn_url":"postgres://10.0.0.1:5433/postgres","role":"replica"}`, host: "10.0.0.1", port: "5433"},
		{name: "ipv6", value: `{"conn_url":"postgres://[fd00::1]:5432/postgres"}`, host: "fd00::1", port: "5432"},
		{name: "no port", value: `{"conn_url":"postgresql://pg-1.local/postgres"}`, host: "pg-1.local", port: "5432"},
		{name: "conn_kwargs", value: `{"conn_kwargs":{"host":"10.0.0.2","port":5434}}`, host: "10.0.0.2", port: "5434"},
		{name: "conn_kwargs string port", value: `{"conn_kwargs":{"host":"10.0.0.2","port":"5434"}}`, host: "10.0.0.2", port: "5434"},
		{name: "unix socket in query", value: `{"conn_url":"postgres:///postgres?host=/var/run/postgresql"}`, wantErr: errUnixSocket},
		{name: "unix socket kwargs", value: `{"conn_kwargs":{"host":"/tmp"}}`, wantErr: errUnixSocket},
		{name: "empty", value: `{"role":"master"}`, wantErr: errNoAddress},
		{name: "wrong scheme", value: `{"conn_url":"http://10.0.0.1:5432/"}`, wantErr: errBadConnURL},
		{name: "bad port", value: `{"conn_url":"postgres://10.0.0.1:99999/postgres"}`, wantErr: errBadPort},
		{name: "not json", value: `garbage`, wantErr: errMemberValue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			host, port, err := memberAddr([]byte(tt.value))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("memberAddr() error = %v, want %v", err, tt.wantErr)
				}

				return
			}

			if err != nil || host != tt.host || port != tt.port {
				t.Errorf("memberAddr() = %q, %q, %v, want %q, %q", host, port, err, tt.host, tt.port)
			}
		})
	}
}

func TestStandbyCluster(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		value       string
		wantStandby bool
		want        []hostPort
	}{
		{name: "not a standby", value: `{"ttl":30}`},
		{name: "null section", value: `{"standby_cluster":null}`},
		{name: "host and port", value: `{"standby_cluster":{"host":"beta-primary","port":5432}}`,
			wantStandby: true, want: []hostPort{{"beta-primary", "5432"}}},
		{name: "default port", value: `{"standby_cluster":{"host":"10.0.0.1"}}`,
			wantStandby: true, want: []hostPort{{"10.0.0.1", "5432"}}},
		{name: "host list, one port", value: `{"standby_cluster":{"host":"a, b","port":"5433"}}`,
			wantStandby: true, want: []hostPort{{"a", "5433"}, {"b", "5433"}}},
		{name: "host list, port list", value: `{"standby_cluster":{"host":"a,b","port":"5433,5434"}}`,
			wantStandby: true, want: []hostPort{{"a", "5433"}, {"b", "5434"}}},
		{name: "restore_command only", value: `{"standby_cluster":{"restore_command":"cp %f %p"}}`,
			wantStandby: true},
		{name: "invalid json", value: `{`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			isStandby, got := standbyCluster([]byte(tt.value))
			if isStandby != tt.wantStandby || !slices.Equal(got, tt.want) {
				t.Errorf("standbyCluster() = %v, %v, want %v, %v", isStandby, got, tt.wantStandby, tt.want)
			}
		})
	}
}

func TestParseLayout(t *testing.T) {
	t.Parallel()

	kvs := []kv{
		{Key: "/service/alpha/members/alpha-2", Value: []byte(`{"conn_url":"postgres://10.0.0.2:5432/postgres"}`)},
		{Key: "/service/alpha/members/alpha-1", Value: []byte(`{"conn_url":"postgres://10.0.0.1:5432/postgres"}`)},
		{Key: "/service/alpha/members/alpha-bad", Value: []byte(`{"role":"replica"}`)},
		{Key: "/service/alpha/initialize", Value: []byte("7300000000000000001\n")},
		{Key: "/service/alpha/leader", Value: []byte("alpha-1")},
		{Key: "/service/alpha/history", Value: []byte("[]")},
		{Key: "/service/beta/config", Value: []byte(`{"standby_cluster":{"host":"vip"}}`)},
		{Key: "/service/beta/members/beta-1", Value: []byte(`{"conn_url":"postgres://10.0.1.1:5432/postgres"}`)},
		{Key: "/service/gone/config", Value: []byte(`{}`)},
		{Key: "/service/gone/initialize", Value: []byte("1")},
		{Key: "/service/citus/1/members/w1", Value: []byte(`{"conn_url":"postgres://10.0.2.1:5432/postgres"}`)},
		{Key: "/service/alpha/members/", Value: nil},
		{Key: "/service2/other/members/o1", Value: []byte(`{"conn_url":"postgres://10.0.3.1:5432/postgres"}`)},
	}

	scopes, skipped := parseLayout(kvs, "/service/")

	names := make([]string, 0, len(scopes))
	for _, s := range scopes {
		names = append(names, s.Name)
	}

	if want := []string{"alpha", "beta"}; !slices.Equal(names, want) {
		t.Fatalf("scopes = %v, want %v", names, want)
	}

	alpha := scopes[0]
	if want := []member{{"alpha-1", "10.0.0.1", "5432"}, {"alpha-2", "10.0.0.2", "5432"}}; !slices.Equal(alpha.Members, want) {
		t.Errorf("alpha members = %v, want %v", alpha.Members, want)
	}

	if alpha.SysID != "7300000000000000001" || alpha.IsStandby {
		t.Errorf("alpha sysid/standby = %q/%v", alpha.SysID, alpha.IsStandby)
	}

	beta := scopes[1]
	if !beta.IsStandby || !slices.Equal(beta.Standby, []hostPort{{"vip", "5432"}}) {
		t.Errorf("beta standby = %v %v", beta.IsStandby, beta.Standby)
	}

	if len(skipped) != 1 || skipped[0].Scope != "alpha" || skipped[0].Member != "alpha-bad" {
		t.Errorf("skipped = %+v, want alpha/alpha-bad", skipped)
	}
}

func TestParseLayoutConsulKeys(t *testing.T) {
	t.Parallel()

	scopes, _ := parseLayout([]kv{
		{Key: "service/alpha/members/a1", Value: []byte(`{"conn_url":"postgres://10.0.0.1:5432/postgres"}`)},
	}, "service/")

	if len(scopes) != 1 || scopes[0].Name != "alpha" {
		t.Fatalf("scopes = %+v, want alpha", scopes)
	}
}
