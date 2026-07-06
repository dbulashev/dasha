package metrics

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	Providers ProvidersConfig
}

// ErrTargetNotMapped is returned when no targets[] entry matches the request.
var ErrTargetNotMapped = errors.New("metrics: target has no label mapping")

// DiscoveryMeta carries the service-discovery attributes used to auto-derive a
// label mapping for a target that is not enumerated in the static Targets list.
type DiscoveryMeta struct {
	Source     string            // "static" | "yandex-mdb" | ...
	ProviderID string            // cloud resource id (e.g. MDB cluster id)
	Labels     map[string]string // discovery labels (e.g. folder_id)
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
}

// NewMatcher precompiles the selector templates. meta is optional and, when
// provided, lets the matcher auto-map discovered clusters absent from Targets.
func NewMatcher(cfg Config, meta MetadataProvider) (*Matcher, error) {
	tmpls := make(map[string]*template.Template, len(cfg.Selectors))

	for key, raw := range cfg.Selectors {
		t, err := template.New(key).Option("missingkey=error").Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("metrics: selector template %q: %w", key, err)
		}

		tmpls[key] = t
	}

	return &Matcher{cfg: cfg, tmpls: tmpls, meta: meta}, nil
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
				Providers: m.cfg.providersFor(t),
			}, nil
		}
	}

	// No static mapping: derive one from discovery metadata. Only non-static
	// (discovered) clusters qualify — a self-managed cluster without a target
	// stays unmapped and the caller falls back to the SQL snapshot.
	if m.meta != nil && m.cfg.autoMapEnabled() {
		if md, ok := m.meta.LookupMeta(cluster, instance); ok && isDiscovered(md.Source) {
			return m.deriveTarget(cluster, instance, md), nil
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
// customization surface for the real label scheme.
func (m *Matcher) deriveTarget(cluster, instance string, md DiscoveryMeta) ResolvedTarget {
	return ResolvedTarget{
		Cluster:   cluster,
		Env:       md.Labels[m.cfg.envLabelKey()],
		Service:   cluster,
		ServiceID: md.ProviderID,
		Host:      instance,
		Container: shortHost(instance),
		Providers: m.cfg.providersFor(nil),
	}
}

// isDiscovered reports whether a cluster came from service discovery (i.e. is
// not a static config entry).
func isDiscovered(source string) bool {
	return source != "" && source != "static"
}

// shortHost returns the first DNS label of an FQDN (rc1a-abc.mdb… -> rc1a-abc).
func shortHost(fqdn string) string {
	if i := strings.IndexByte(fqdn, '.'); i > 0 {
		return fqdn[:i]
	}

	return fqdn
}

// Selector renders the inner label selector (without braces) for a provider in
// a given role.
func (m *Matcher) Selector(p Provider, role Role, rt ResolvedTarget) (string, error) {
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
}

// roleAssignment pairs a role with its configured provider.
type roleAssignment struct {
	role     Role
	provider Provider
}

// Validate confirms each role's selector matches exactly one live series.
func (m *Matcher) Validate(ctx context.Context, client DatasourceClient, cluster, instance string) (Diagnostics, error) {
	rt, err := m.Resolve(cluster, instance)
	if err != nil {
		return Diagnostics{}, err
	}

	diag := Diagnostics{Target: cluster + "/" + instance}

	assignments := []roleAssignment{
		{RoleCore, rt.Providers.Core},
		{RolePooler, rt.Providers.Pooler},
		{RoleHost, rt.Providers.Host},
	}

	for _, a := range assignments {
		diag.Roles = append(diag.Roles, m.validateRole(ctx, client, rt, a))
	}

	return diag, nil
}

func (m *Matcher) validateRole(ctx context.Context, client DatasourceClient, rt ResolvedTarget, a roleAssignment) RoleDiagnostic {
	rd := RoleDiagnostic{Role: a.role, Provider: a.provider}

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
