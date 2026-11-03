package metrics

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"
)

// ResolvedTarget is a Dasha target projected onto datasource identifiers and
// the providers chosen for each role.
type ResolvedTarget struct {
	Cluster   string // Dasha cluster name (selector var {{.Cluster}})
	Env       string
	Service   string // {{.Service}}: cluster name for discovered targets (most YC/pgscv metrics key by name)
	ServiceID string // {{.ServiceID}}: cloud resource id (MDB cluster UUID), for metrics keyed by id
	Host      string
	Container string
	Instance  string // Dasha instance name
	HostShort string // first DNS label of Host; an IP stays as is
	Port      string // PostgreSQL port; empty for targets[] entries
	Providers ProvidersConfig
	Scrape    map[Role]string // instance/job selector found by the anchor lookup
}

// ErrTargetNotMapped is returned when no targets[] entry matches the request.
var ErrTargetNotMapped = errors.New("metrics: target has no label mapping")

// Patroni discovery values; metrics cannot import config, which imports metrics.
const (
	sourcePatroni     = "patroni"       // config.SourcePatroni
	labelPatroniScope = "patroni.scope" // config.LabelPatroniScope
)

// DiscoveryMeta carries the service-discovery attributes used to auto-derive a
// label mapping for a target that is not enumerated in the static Targets list.
type DiscoveryMeta struct {
	Source     string            // "static" | "yandex-mdb" | ...
	ProviderID string            // cloud resource id (e.g. MDB cluster id)
	Labels     map[string]string // discovery labels (e.g. folder_id)
	Alias      string            // host display name, e.g. the Patroni member name
	Address    string            // address to connect to; empty: the instance name itself
	Port       string            // PostgreSQL port; empty: 5432
}

// MetadataProvider supplies DiscoveryMeta for a Dasha (cluster, instance), so
// the matcher can map dynamically-discovered clusters that the static Targets
// list does not enumerate. Optional: a nil provider disables auto-mapping.
type MetadataProvider interface {
	LookupMeta(cluster, instance string) (DiscoveryMeta, bool)
}

// Matcher resolves Dasha targets to provider selectors and validates them
// against the datasource.
type Matcher struct {
	cfg   Config
	tmpls map[string]*template.Template
	meta  MetadataProvider // optional: enables discovery-driven auto-mapping
	now   func() time.Time

	anchorMu sync.Mutex
	anchors  map[Provider]anchorSnap
}

// selectorFuncs are available in selector templates.
var selectorFuncs = template.FuncMap{"promre": promRegexLiteral}

// promRegexLiteral quotes s as a regex literal inside a PromQL string.
func promRegexLiteral(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(regexp.QuoteMeta(s))
}

// NewMatcher precompiles the selector templates. meta is optional and, when
// provided, lets the matcher auto-map discovered clusters absent from Targets.
func NewMatcher(cfg Config, meta MetadataProvider) (*Matcher, error) {
	tmpls := make(map[string]*template.Template, len(cfg.Selectors))

	for key, raw := range cfg.Selectors {
		t, err := template.New(key).Funcs(selectorFuncs).Option("missingkey=error").Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("metrics: selector template %q: %w", key, err)
		}

		tmpls[key] = t
	}

	return &Matcher{cfg: cfg, tmpls: tmpls, meta: meta, now: time.Now, anchors: map[Provider]anchorSnap{}}, nil
}

// Resolve finds the mapping for a Dasha (cluster, instance) target. A static
// Targets[] entry wins; otherwise, for service-discovered clusters, the mapping
// is derived from discovery metadata so they need no hand-written target.
func (m *Matcher) Resolve(cluster, instance string) (ResolvedTarget, error) {
	for i := range m.cfg.Targets {
		t := &m.cfg.Targets[i]
		if t.Cluster == cluster && t.Instance == instance {
			return ResolvedTarget{
				Cluster:   cluster,
				Env:       t.Env,
				Service:   t.Service,
				ServiceID: t.Service,
				Host:      t.Host,
				Container: t.Container,
				Instance:  instance,
				HostShort: shortHost(t.Host),
				Providers: m.cfg.providersFor(t),
			}, nil
		}
	}

	// No static mapping: derive one from discovery metadata. Only non-static
	// (discovered) clusters qualify — a self-managed cluster without a target
	// stays unmapped and the caller falls back to the SQL snapshot.
	if m.meta != nil {
		if md, ok := m.meta.LookupMeta(cluster, instance); ok {
			switch {
			case isDiscovered(md.Source) && m.cfg.autoMapEnabled():
				return m.deriveTarget(cluster, instance, md), nil
			case !isDiscovered(md.Source) && m.cfg.autoMapStaticEnabled():
				return ResolvedTarget{
					Cluster:   cluster,
					Host:      instance,
					Instance:  instance,
					HostShort: shortHost(instance),
					Port:      cmp.Or(md.Port, defaultPGPort),
					Providers: m.cfg.providersFor(nil),
				}, nil
			}
		}
	}

	return ResolvedTarget{}, fmt.Errorf("%w: %s/%s", ErrTargetNotMapped, cluster, instance)
}

// deriveTarget builds a ResolvedTarget from discovery metadata. Service defaults
// to the cluster name (Cluster), since most YC MDB + pgscv metric schemes key by
// name (resource_id/subcluster_name/service_id = cluster name); the MDB cluster
// UUID is exposed separately as ServiceID for schemes that key by id. Host is the
// FQDN, Container the short hostname, Env the configured discovery label (default
// folder_id). Providers come from providers_default; selector templates stay the
// customization surface for the real label scheme. Patroni clusters key by scope
// (without the entry prefix) and member name instead.
func (m *Matcher) deriveTarget(cluster, instance string, md DiscoveryMeta) ResolvedTarget {
	if md.Source == sourcePatroni {
		scope := cmp.Or(md.Labels[labelPatroniScope], cluster)
		host := cmp.Or(md.Address, instance)

		return ResolvedTarget{
			Cluster:   cluster,
			Env:       md.Labels[m.cfg.envLabelKey()],
			Service:   scope,
			ServiceID: scope,
			Host:      host,
			Container: cmp.Or(md.Alias, shortHost(instance)),
			Instance:  instance,
			HostShort: shortHost(host),
			Port:      cmp.Or(md.Port, defaultPGPort),
			Providers: m.cfg.providersFor(nil),
		}
	}

	return ResolvedTarget{
		Cluster:   cluster,
		Env:       md.Labels[m.cfg.envLabelKey()],
		Service:   cluster,
		ServiceID: md.ProviderID,
		Host:      instance,
		Container: shortHost(instance),
		Instance:  instance,
		HostShort: shortHost(instance),
		Port:      cmp.Or(md.Port, defaultPGPort),
		Providers: m.cfg.providersFor(nil),
	}
}

const defaultPGPort = "5432"

// isDiscovered reports whether a cluster came from service discovery (i.e. is
// not a static config entry).
func isDiscovered(source string) bool {
	return source != "" && source != "static"
}

// shortHost returns the first DNS label of an FQDN (rc1a-abc.mdb… -> rc1a-abc).
// An IP address, with or without a port, is returned unchanged.
func shortHost(fqdn string) string {
	host := fqdn
	if h, _, err := net.SplitHostPort(fqdn); err == nil {
		host = h
	}

	if net.ParseIP(host) != nil {
		return fqdn
	}

	if i := strings.IndexByte(fqdn, '.'); i > 0 {
		return fqdn[:i]
	}

	return fqdn
}

// Selector renders the inner label selector (without braces) for a provider in
// a given role.
func (m *Matcher) Selector(p Provider, role Role, rt ResolvedTarget) (string, error) {
	if s := rt.Scrape[role]; s != "" {
		return s, nil
	}

	key := selectorKey(p, role)

	t, ok := m.tmpls[key]
	if !ok {
		return "", fmt.Errorf("metrics: no selector template for %q", key)
	}

	var sb strings.Builder
	if err := t.Execute(&sb, rt); err != nil {
		return "", fmt.Errorf("metrics: render selector %q: %w", key, err)
	}

	return sb.String(), nil
}

// selectorKey maps a (provider, role) pair to a Selectors map key.
func selectorKey(p Provider, role Role) string {
	switch p {
	case ProviderPgSCV:
		return SelectorPgSCV
	case ProviderPgBouncer:
		return SelectorPgBouncer
	case ProviderPgSCVSystem:
		return SelectorPgSCVSystem
	case ProviderPostgresExporter, ProviderNodeExporter, ProviderPgBouncerExporter:
		return string(p)
	case ProviderYCNative:
		if role == RolePooler {
			return SelectorYCNativePooler
		}

		return SelectorYCNativeHost
	default:
		return SelectorPgSCV
	}
}

// Diagnostics is the result of validating a target against the datasource.
type Diagnostics struct {
	Target string
	Roles  []RoleDiagnostic
}

// RoleDiagnostic reports the match for one role/provider.
type RoleDiagnostic struct {
	Role     Role
	Provider Provider
	Metric   string
	Selector string
	Matched  int
	OK       bool
	Sample   map[string]string
	Err      string
	Match    string // MatchStatic | MatchExplicit | MatchAnchor | MatchTemplate
	Missing  []MissingSignal
}

// MissingSignal is a signal the provider can serve that the datasource did not
// return; Flag names the exporter flag enabling it, when known.
type MissingSignal struct {
	Signal SignalKind
	Flag   string
}

// roleAssignment pairs a role with its configured provider.
type roleAssignment struct {
	role     Role
	provider Provider
}

// Validate confirms each role's selector matches exactly one live series. The
// anchor lookup bypasses its cache and refreshes it.
func (m *Matcher) Validate(ctx context.Context, client DatasourceClient, cluster, instance string) (Diagnostics, error) {
	rt, err := m.ResolveCtx(ctx, client, cluster, instance, true)
	if err != nil {
		return Diagnostics{}, err
	}

	diag := Diagnostics{Target: cluster + "/" + instance}

	for _, a := range roleAssignments(rt.Providers) {
		if a.provider == ProviderNone {
			continue
		}

		diag.Roles = append(diag.Roles, m.validateRole(ctx, client, rt, a))
	}

	return diag, nil
}

func (m *Matcher) validateRole(ctx context.Context, client DatasourceClient, rt ResolvedTarget, a roleAssignment) RoleDiagnostic {
	rd := RoleDiagnostic{Role: a.role, Provider: a.provider, Match: m.matchKind(a.provider, a.role, rt)}

	metric := validationMetricFor(a.provider, a.role)
	if metric == "" {
		rd.Err = "no validation metric defined for provider"

		return rd
	}

	rd.Metric = metric

	sel, err := m.Selector(a.provider, a.role, rt)
	if err != nil {
		rd.Err = err.Error()

		return rd
	}

	rd.Selector = sel

	samples, err := client.QueryInstant(ctx, fmt.Sprintf("%s{%s}", metric, sel), time.Time{})
	if err != nil {
		rd.Err = err.Error()

		return rd
	}

	rd.Matched = len(samples)
	rd.OK = rd.Matched == 1

	if rd.Matched > 0 {
		rd.Sample = samples[0].Labels
	}

	return rd
}

// validationMetricFor picks the per-role validation metric, special-casing the
// YC pooler (which has its own liveness series).
func validationMetricFor(p Provider, role Role) string {
	if p == ProviderYCNative && role == RolePooler {
		return "pooler_is_alive"
	}

	return ValidationMetric(p)
}
