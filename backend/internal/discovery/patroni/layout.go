package patroni

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const defaultPGPort = "5432"

var (
	errNoAddress   = errors.New("no conn_url or conn_kwargs.host")
	errUnixSocket  = errors.New("unix-socket address")
	errBadConnURL  = errors.New("invalid conn_url")
	errBadPort     = errors.New("invalid port")
	errMemberValue = errors.New("member value is not JSON")
)

type member struct {
	Name string
	Host string
	Port string
}

type hostPort struct {
	Host string
	Port string
}

type scopeData struct {
	Name      string
	Members   []member   // sorted by Name
	SysID     string     // <scope>/initialize: PostgreSQL system identifier
	Standby   []hostPort // standby_cluster host/port; nil when not set
	IsStandby bool       // standby_cluster present, with or without host
}

// skippedMember is a member key whose value gave no usable address.
type skippedMember struct {
	Scope  string
	Member string
	Reason error
}

// parseLayout groups keys under prefix into scopes, sorted by name. Only
// scopes with at least one valid member are returned; keys outside the
// Patroni layout, including Citus groups, are ignored.
func parseLayout(kvs []kv, prefix string) ([]scopeData, []skippedMember) {
	byName := make(map[string]*scopeData)

	get := func(name string) *scopeData {
		s, ok := byName[name]
		if !ok {
			s = &scopeData{Name: name} //nolint:exhaustruct
			byName[name] = s
		}

		return s
	}

	var skipped []skippedMember

	for _, item := range kvs {
		rest, ok := strings.CutPrefix(item.Key, prefix)
		if !ok {
			continue
		}

		parts := strings.Split(rest, "/")
		if parts[0] == "" {
			continue
		}

		switch {
		case len(parts) == 3 && parts[1] == "members" && parts[2] != "":
			host, port, err := memberAddr(item.Value)
			if err != nil {
				skipped = append(skipped, skippedMember{Scope: parts[0], Member: parts[2], Reason: err})

				continue
			}

			s := get(parts[0])
			s.Members = append(s.Members, member{Name: parts[2], Host: host, Port: port})
		case len(parts) == 2 && parts[1] == "config":
			s := get(parts[0])
			s.IsStandby, s.Standby = standbyCluster(item.Value)
		case len(parts) == 2 && parts[1] == "initialize":
			get(parts[0]).SysID = strings.TrimSpace(string(item.Value))
		}
	}

	scopes := make([]scopeData, 0, len(byName))

	for _, s := range byName {
		if len(s.Members) == 0 {
			continue
		}

		slices.SortFunc(s.Members, func(a, b member) int { return strings.Compare(a.Name, b.Name) })
		scopes = append(scopes, *s)
	}

	slices.SortFunc(scopes, func(a, b scopeData) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(skipped, func(a, b skippedMember) int {
		return strings.Compare(a.Scope+"/"+a.Member, b.Scope+"/"+b.Member)
	})

	return scopes, skipped
}

// memberAddr extracts host and port from a member's JSON value. Errors never
// carry conn_url: it may hold credentials.
func memberAddr(value []byte) (string, string, error) {
	var m struct {
		ConnURL    string `json:"conn_url"`
		ConnKwargs struct {
			Host string          `json:"host"`
			Port json.RawMessage `json:"port"`
		} `json:"conn_kwargs"`
	}

	if err := json.Unmarshal(value, &m); err != nil {
		return "", "", errMemberValue
	}

	var host, port string

	if m.ConnURL != "" {
		u, err := url.Parse(m.ConnURL)
		if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
			return "", "", errBadConnURL
		}

		q := u.Query()
		host, port = cmp.Or(u.Hostname(), q.Get("host")), cmp.Or(u.Port(), q.Get("port"))
	} else {
		host, port = m.ConnKwargs.Host, rawScalar(m.ConnKwargs.Port)
	}

	// libpq accepts comma-separated lists; the first entry is the member itself.
	host, port = firstOf(host), firstOf(port)

	switch {
	case host == "":
		return "", "", errNoAddress
	case strings.HasPrefix(host, "/"):
		return "", "", errUnixSocket
	}

	if port == "" {
		port = defaultPGPort
	}

	if !validPort(port) {
		return "", "", errBadPort
	}

	return host, port, nil
}

// standbyCluster reads standby_cluster from a scope's dynamic config. Patroni
// allows host to be a comma-separated list; port is one value or a list of the
// same length.
func standbyCluster(value []byte) (bool, []hostPort) {
	var dyn struct {
		StandbyCluster json.RawMessage `json:"standby_cluster"`
	}

	if err := json.Unmarshal(value, &dyn); err != nil {
		return false, nil
	}

	raw := bytes.TrimSpace(dyn.StandbyCluster)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return false, nil
	}

	var sc struct {
		Host string          `json:"host"`
		Port json.RawMessage `json:"port"`
	}

	if err := json.Unmarshal(raw, &sc); err != nil {
		return true, nil
	}

	hosts := splitList(sc.Host)
	if len(hosts) == 0 {
		return true, nil
	}

	ports := splitList(rawScalar(sc.Port))

	out := make([]hostPort, 0, len(hosts))

	for i, h := range hosts {
		port := defaultPGPort

		switch {
		case len(ports) == len(hosts):
			port = ports[i]
		case len(ports) > 0:
			port = ports[0]
		}

		out = append(out, hostPort{Host: h, Port: port})
	}

	return true, out
}

// rawScalar returns a JSON string or number as text; anything else is "".
func rawScalar(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}

	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}

	return ""
}

func splitList(s string) []string {
	var out []string

	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}

	return out
}

func firstOf(list string) string {
	if items := splitList(list); len(items) > 0 {
		return items[0]
	}

	return ""
}

func validPort(p string) bool {
	n, err := strconv.Atoi(p)

	return err == nil && n > 0 && n <= 65535
}
