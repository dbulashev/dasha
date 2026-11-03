package metrics

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// anchorSpec reads every scrape of one exporter type in a single query; addr
// pulls the database host (and port, when known) out of a series.
type anchorSpec struct {
	query string
	addr  func(labels map[string]string) (host, port string)
}

var anchorSpecs = map[Provider]anchorSpec{
	ProviderPostgresExporter: {
		query: `count by (instance, job, server) (pg_static)`,
		addr: func(l map[string]string) (string, string) {
			if h, p, err := net.SplitHostPort(l["server"]); err == nil {
				return h, p
			}

			return l["server"], ""
		},
	},
	ProviderNodeExporter: {
		query: `count by (instance, job, nodename) (node_uname_info)`,
		addr:  func(l map[string]string) (string, string) { return l["nodename"], "" },
	},
	ProviderPgBouncerExporter: {
		query: `count by (instance, job, host, port) (pgbouncer_databases_pool_size{` + pgbNoAdm + `})`,
		addr:  func(l map[string]string) (string, string) { return l["host"], l["port"] },
	},
}

type anchorSnap struct {
	samples []Sample
	expires time.Time
	failed  bool
}

// Anchor cache lifetimes: anchorTTL for scrapes found, anchorErrorTTL for a failed query.
const (
	anchorTTL      = time.Minute
	anchorErrorTTL = 30 * time.Second
)

// usesAnchor reports whether a provider's scrape is looked up by address: it
// has an anchor metric and the user kept the fallback selector template.
func (m *Matcher) usesAnchor(p Provider) bool {
	_, ok := anchorSpecs[p]

	return ok && m.cfg.Selectors[string(p)] == exporterFallbackSelector
}

// ResolveCtx is Resolve plus the anchor lookup: a role served by a stock
// exporter gets the instance/job of the one scrape whose address metric names
// this host. fresh bypasses the anchor cache.
func (m *Matcher) ResolveCtx(ctx context.Context, c DatasourceClient, cluster, instance string, fresh bool) (ResolvedTarget, error) {
	rt, err := m.Resolve(cluster, instance)
	if err != nil {
		return rt, err
	}

	for _, a := range roleAssignments(rt.Providers) {
		if !m.usesAnchor(a.provider) {
			continue
		}

		samples, ok := m.anchorSamples(ctx, c, a.provider, fresh)
		if !ok {
			continue
		}

		if sel := scrapeSelector(samples, anchorSpecs[a.provider], rt); sel != "" {
			if rt.Scrape == nil {
				rt.Scrape = make(map[Role]string, 1)
			}

			rt.Scrape[a.role] = sel
		}
	}

	return rt, nil
}

// anchorSamples returns the provider's anchor series. A datasource failure on a
// non-fresh call is cached; meanwhile the role falls back to its template.
func (m *Matcher) anchorSamples(ctx context.Context, c DatasourceClient, p Provider, fresh bool) ([]Sample, bool) {
	now := m.now()

	if !fresh {
		m.anchorMu.Lock()
		snap, ok := m.anchors[p]
		m.anchorMu.Unlock()

		if ok && now.Before(snap.expires) {
			return snap.samples, !snap.failed
		}
	}

	samples, err := c.QueryInstant(ctx, anchorSpecs[p].query, time.Time{})
	if err != nil {
		if !fresh && ctx.Err() == nil {
			m.anchorMu.Lock()
			m.anchors[p] = anchorSnap{failed: true, expires: now.Add(anchorErrorTTL)}
			m.anchorMu.Unlock()
		}

		return nil, false
	}

	m.anchorMu.Lock()
	m.anchors[p] = anchorSnap{samples: samples, expires: now.Add(anchorTTL)}
	m.anchorMu.Unlock()

	return samples, true
}

// scrapeSelector returns `instance="…",job="…"` when exactly one scrape names
// the target's host, "" otherwise. Several scrapes of one host are told apart by
// the PostgreSQL port.
func scrapeSelector(samples []Sample, spec anchorSpec, rt ResolvedTarget) string {
	type scrape struct{ instance, job string }

	byHost, byPort := map[scrape]bool{}, map[scrape]bool{}

	for _, s := range samples {
		host, port := spec.addr(s.Labels)
		if !hostMatches(host, rt) {
			continue
		}

		sc := scrape{s.Labels["instance"], s.Labels["job"]}
		byHost[sc] = true

		if port != "" && port == rt.Port {
			byPort[sc] = true
		}
	}

	found := byHost
	if len(found) > 1 {
		found = byPort
	}

	if len(found) != 1 {
		return ""
	}

	for s := range found {
		return fmt.Sprintf("instance=%q,job=%q", s.instance, s.job)
	}

	return ""
}

// hostMatches compares an exporter-reported address with the target's names,
// case-insensitively. Two FQDNs must be equal; a short name matches the first
// label of the other side. A Patroni member alias matches by first label. IPs
// compare as strings.
func hostMatches(host string, rt ResolvedTarget) bool {
	if host == "" {
		return false
	}

	fqdn := isFQDN(host) && (isFQDN(rt.Host) || isFQDN(rt.Instance))

	for _, name := range []string{rt.Host, rt.Instance} {
		if name == "" {
			continue
		}

		if strings.EqualFold(host, name) || !fqdn && strings.EqualFold(shortHost(host), shortHost(name)) {
			return true
		}
	}

	alias := rt.Container
	if alias == "" || strings.EqualFold(alias, shortHost(rt.Host)) || strings.EqualFold(alias, shortHost(rt.Instance)) {
		return false
	}

	return strings.EqualFold(shortHost(host), shortHost(alias))
}

func isFQDN(name string) bool {
	return strings.Contains(name, ".") && net.ParseIP(name) == nil
}

// roleAssignments lists the roles in diagnostic order.
func roleAssignments(p ProvidersConfig) []roleAssignment {
	return []roleAssignment{
		{RoleCore, p.Core},
		{RolePooler, p.Pooler},
		{RoleHost, p.Host},
	}
}

// matchKind names how a role's selector was chosen (see RoleDiagnostic.Match).
func (m *Matcher) matchKind(p Provider, role Role, rt ResolvedTarget) string {
	if _, ok := anchorSpecs[p]; !ok {
		return MatchStatic
	}

	switch {
	case !m.usesAnchor(p):
		return MatchExplicit
	case rt.Scrape[role] != "":
		return MatchAnchor
	default:
		return MatchTemplate
	}
}

// RoleDiagnostic.Match values.
const (
	MatchStatic   = "static"
	MatchExplicit = "explicit"
	MatchAnchor   = "anchor"
	MatchTemplate = "template"
)
