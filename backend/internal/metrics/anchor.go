package metrics

import (
	"cmp"
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
}

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

// anchorSamples returns the provider's anchor series. A failed query is not
// cached: the role falls back to its template until the next call.
func (m *Matcher) anchorSamples(ctx context.Context, c DatasourceClient, p Provider, fresh bool) ([]Sample, bool) {
	now := m.now()

	if !fresh {
		m.anchorMu.Lock()
		snap, ok := m.anchors[p]
		m.anchorMu.Unlock()

		if ok && now.Before(snap.expires) {
			return snap.samples, true
		}
	}

	samples, err := c.QueryInstant(ctx, anchorSpecs[p].query, time.Time{})
	if err != nil {
		return nil, false
	}

	ttl := cmp.Or(m.cfg.Baseline.CacheTTL, Default().Baseline.CacheTTL)

	m.anchorMu.Lock()
	m.anchors[p] = anchorSnap{samples: samples, expires: now.Add(ttl)}
	m.anchorMu.Unlock()

	return samples, true
}

// scrapeSelector returns `instance="…",job="…"` when exactly one scrape names
// the target's host, "" otherwise.
func scrapeSelector(samples []Sample, spec anchorSpec, rt ResolvedTarget) string {
	type scrape struct{ instance, job string }

	found := map[scrape]bool{}

	for _, s := range samples {
		if host, port := spec.addr(s.Labels); hostMatches(host, port, rt) {
			found[scrape{s.Labels["instance"], s.Labels["job"]}] = true
		}
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
// case-insensitively, in full or short form. IPs compare as strings; the port
// only when both sides know it.
func hostMatches(host, port string, rt ResolvedTarget) bool {
	if host == "" {
		return false
	}

	if port != "" && rt.Port != "" && port != rt.Port {
		return false
	}

	short := shortHost(host)

	for _, name := range []string{rt.Host, rt.Instance, rt.Container} {
		if name == "" {
			continue
		}

		if strings.EqualFold(host, name) || strings.EqualFold(host, shortHost(name)) || strings.EqualFold(short, name) {
			return true
		}
	}

	return false
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
