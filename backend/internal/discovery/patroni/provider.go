package patroni

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/discovery/filter"
	"github.com/dbulashev/dasha/internal/discovery/pgdb"
)

const (
	scopeBudget  = 30 * time.Second
	scopeWorkers = 8
)

// Provider discovers the Patroni clusters of one DCS namespace.
type Provider struct {
	entry      string
	cfg        Config
	namespace  string // normalized, for labels and logs
	prefix     string // key prefix in the DCS
	store      store
	list       pgdb.Lister
	scopes     *filter.Filter
	base       scopePG
	overrides  map[string]scopePG // by lower-cased scope name
	prefixName bool
	interval   time.Duration
	logger     *zap.Logger

	// Owned by the discovery loop, one cycle at a time.
	state      map[string]scopeState // nil until the first successful cycle
	warned     map[string]bool       // layout warnings of the last cycle
	noOverride map[string]bool       // overrides already reported as unmatched
}

type scopeState struct {
	Databases []string // last successful list, unfiltered
	Listed    bool     // Databases holds a successful list
	Sticky    string   // member that answered last
	Hosts     []string // published
	Published []string // databases after filtering
}

type scopePlan struct {
	data    scopeData
	pg      scopePG
	members []member // one per host
}

type listResult struct {
	names  []string
	sticky string
	err    error
}

// NewProvider decodes a patroni discovery block and creates the DCS client.
// prefixName qualifies cluster names with the entry name.
func NewProvider(name string, raw map[string]any, prefixName bool, logger *zap.Logger) (*Provider, error) {
	p, err := newProvider(name, raw, prefixName, logger)
	if err != nil {
		return nil, err
	}

	p.store, err = newStore(p.cfg, logger)
	if err != nil {
		return nil, err
	}

	fields := []zap.Field{
		zap.String("entry", name),
		zap.String("dcs", p.cfg.DCS),
		zap.String("namespace", p.namespace),
		zap.String("scope", p.cfg.Scope),
	}

	if p.cfg.ExcludeScope != nil {
		fields = append(fields, zap.String("exclude_scope", *p.cfg.ExcludeScope))
	}

	if p.cfg.DCS == dcsEtcd3 {
		fields = append(fields, zap.Strings("endpoints", p.cfg.Etcd3.Endpoints))
	} else {
		fields = append(fields, zap.String("address", p.cfg.Consul.Address))
	}

	logger.Info("patroni discovery configured", fields...)

	return p, nil
}

// newProvider builds everything but the DCS client.
func newProvider(name string, raw map[string]any, prefixName bool, logger *zap.Logger) (*Provider, error) {
	cfg, unused, err := config.DecodeDiscoveryConfig[Config](raw)
	if err != nil {
		return nil, err
	}

	if len(unused) > 0 {
		logger.Warn("unknown keys in discovery config, ignored",
			zap.String("entry", name),
			zap.Strings("keys", unused),
		)
	}

	cfg = cfg.withDefaults()

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	for _, other := range []string{dcsEtcd3, dcsConsul} {
		if other != cfg.DCS && raw[other] != nil {
			logger.Warn("settings of another dcs, ignored",
				zap.String("entry", name),
				zap.String("dcs", cfg.DCS),
				zap.String("ignored", other),
			)
		}
	}

	interval, raised := config.DiscoveryInterval(cfg.RefreshInterval)
	if raised {
		logger.Warn("refresh_interval is below the minimum, raised",
			zap.String("entry", name),
			zap.Duration("configured", cfg.RefreshInterval),
			zap.Duration("interval", interval),
		)
	}

	scopes, err := filter.New(cfg.Scope, nil, cfg.ExcludeScope, nil)
	if err != nil {
		return nil, fmt.Errorf("compile scope filter: %w", err)
	}

	base, overrides, err := resolvePG(name, cfg, logger)
	if err != nil {
		return nil, err
	}

	return &Provider{
		entry:      name,
		cfg:        cfg,
		namespace:  normalizeNamespace(cfg.Namespace),
		prefix:     namespacePrefix(cfg.Namespace, cfg.DCS),
		store:      nil,
		list:       pgdb.ListDatabases,
		scopes:     scopes,
		base:       base,
		overrides:  overrides,
		prefixName: prefixName,
		interval:   interval,
		logger:     logger,
		state:      nil,
		warned:     nil,
		noOverride: make(map[string]bool),
	}, nil
}

// Interval returns the configured refresh interval.
func (p *Provider) Interval() time.Duration {
	return p.interval
}

// Close releases the DCS client.
func (p *Provider) Close() error {
	return p.store.Close()
}

// Discover reads the namespace, lists each scope's databases and returns one
// cluster per scope, sorted by name. Only a DCS failure fails the cycle.
func (p *Provider) Discover(ctx context.Context) ([]config.Cluster, error) {
	dcsCtx, cancel := context.WithTimeout(ctx, dcsTimeout)
	kvs, err := p.store.list(dcsCtx, p.prefix)

	cancel()

	if err != nil {
		return nil, fmt.Errorf("read %s namespace %q: %w", p.cfg.DCS, p.namespace, err)
	}

	all, skipped := parseLayout(kvs, p.prefix)

	scopes := make([]scopeData, 0, len(all))

	for _, s := range all {
		if p.scopes.MatchName(s.Name) {
			scopes = append(scopes, s)
		}
	}

	warned := make(map[string]bool)

	for _, sk := range skipped {
		if p.scopes.MatchName(sk.Scope) {
			p.warnOnce(warned, "member/"+sk.Scope+"/"+sk.Member, "patroni member skipped, no usable address",
				zap.String("entry", p.entry),
				zap.String("scope", sk.Scope),
				zap.String("member", sk.Member),
				zap.String("reason", sk.Reason.Error()),
			)
		}
	}

	plans := p.plan(scopes, warned)
	results := p.listAll(ctx, plans)

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("patroni discovery: %w", err)
	}

	clusters, next := p.build(plans, results)

	p.logChanges(next)
	p.reportUnmatchedOverrides(scopes)

	p.state = next
	p.warned = warned

	return clusters, nil
}

// warnOnce logs a warning unless the previous cycle logged the same key.
func (p *Provider) warnOnce(seen map[string]bool, key, msg string, fields ...zap.Field) {
	seen[key] = true

	if !p.warned[key] {
		p.logger.Warn(msg, fields...)
	}
}

func (p *Provider) pgFor(scope string) scopePG {
	if pg, ok := p.overrides[strings.ToLower(scope)]; ok {
		return pg
	}

	return p.base
}

// plan resolves settings per scope and keeps one member per host: the host is
// the pool key, so a second member on it would collide.
func (p *Provider) plan(scopes []scopeData, warned map[string]bool) []scopePlan {
	plans := make([]scopePlan, 0, len(scopes))

	for _, s := range scopes {
		seen := make(map[string]bool, len(s.Members))
		members := make([]member, 0, len(s.Members))

		for _, m := range s.Members {
			if seen[m.Host] {
				p.warnOnce(warned, "host/"+s.Name+"/"+m.Name, "patroni member shares a host with another member, skipped",
					zap.String("entry", p.entry),
					zap.String("scope", s.Name),
					zap.String("member", m.Name),
					zap.String("host", m.Host),
				)

				continue
			}

			seen[m.Host] = true

			members = append(members, m)
		}

		plans = append(plans, scopePlan{data: s, pg: p.pgFor(s.Name), members: members})
	}

	return plans
}

// listAll lists databases of every scope in parallel. Each slot is written by
// one goroutine; p.state is only read until Wait returns.
func (p *Provider) listAll(ctx context.Context, plans []scopePlan) []listResult {
	results := make([]listResult, len(plans))

	var g errgroup.Group

	g.SetLimit(scopeWorkers)

	for i := range plans {
		g.Go(func() error {
			results[i] = p.listScope(ctx, plans[i])

			return nil
		})
	}

	_ = g.Wait()

	return results
}

func (p *Provider) listScope(ctx context.Context, pl scopePlan) listResult {
	ctx, cancel := context.WithTimeout(ctx, scopeBudget)
	defer cancel()

	sticky := p.state[pl.data.Name].Sticky
	start := 0
	targets := make([]pgdb.Target, 0, len(pl.members))

	for i, m := range pl.members {
		if m.Name == sticky {
			start = i
		}

		targets = append(targets, pgdb.Target{
			Host:     m.Host,
			Port:     cmp.Or(pl.pg.Port, m.Port),
			User:     pl.pg.User,
			Password: pl.pg.Password,
			Database: pl.pg.BootstrapDB,
		})
	}

	names, idx, err := pgdb.ListFirst(ctx, p.list, targets, start)
	if err != nil {
		return listResult{names: nil, sticky: "", err: err}
	}

	return listResult{names: names, sticky: pl.members[idx].Name, err: nil}
}

func (p *Provider) clusterName(scope string) config.ClusterName {
	if p.prefixName {
		return config.ClusterName(p.entry + "_" + scope)
	}

	return config.ClusterName(scope)
}

func (p *Provider) build(plans []scopePlan, results []listResult) ([]config.Cluster, map[string]scopeState) {
	names := make(map[string]config.ClusterName, len(plans))
	data := make([]scopeData, 0, len(plans))

	for _, pl := range plans {
		names[pl.data.Name] = p.clusterName(pl.data.Name)
		data = append(data, pl.data)
	}

	standby := standbyOf(data, names)

	clusters := make([]config.Cluster, 0, len(plans))
	next := make(map[string]scopeState, len(plans))

	for i, pl := range plans {
		st := p.databasesState(pl.data.Name, results[i])

		databases := make([]config.Database, 0, len(st.Databases))
		for _, db := range st.Databases {
			if pl.pg.filter.MatchDb(db) {
				databases = append(databases, config.Database(db))
				st.Published = append(st.Published, db)
			}
		}

		hosts := make([]config.Host, 0, len(pl.members))
		meta := make(map[config.Host]config.HostMeta, len(pl.members))

		for _, m := range pl.members {
			h := config.Host(m.Host)
			hosts = append(hosts, h)
			st.Hosts = append(st.Hosts, m.Host)

			hm := config.HostMeta{Port: "", Alias: m.Name}
			if pl.pg.Port == "" {
				hm.Port = m.Port
			}

			meta[h] = hm
		}

		labels := map[string]string{
			"patroni.dcs":       p.cfg.DCS,
			"patroni.namespace": p.namespace,
			"patroni.scope":     pl.data.Name,
			"bootstrap_db":      pl.pg.BootstrapDB,
		}

		if primary, ok := standby[pl.data.Name]; ok {
			labels[config.LabelPatroniStandbyOf] = primary.String()
		} else if pl.data.IsStandby {
			p.logger.Debug("standby cluster without a matching primary",
				zap.String("entry", p.entry),
				zap.String("scope", pl.data.Name),
			)
		}

		clusters = append(clusters, config.Cluster{ //nolint:exhaustruct
			Name:       names[pl.data.Name],
			UserName:   pl.pg.User,
			Password:   pl.pg.Password,
			Port:       pl.pg.Port,
			Databases:  databases,
			Hosts:      hosts,
			HostMeta:   meta,
			Source:     config.SourcePatroni,
			ProviderID: p.entry,
			Labels:     labels,
		})

		next[pl.data.Name] = st
	}

	slices.SortFunc(clusters, func(a, b config.Cluster) int { return strings.Compare(string(a.Name), string(b.Name)) })

	return clusters, next
}

// databasesState falls back to the last successful list when no member answered.
func (p *Provider) databasesState(scope string, r listResult) scopeState {
	prev := p.state[scope]

	if r.err == nil {
		return scopeState{Databases: r.names, Listed: true, Sticky: r.sticky} //nolint:exhaustruct
	}

	if prev.Listed {
		p.logger.Warn("no patroni member answered, keeping the last database list",
			zap.String("entry", p.entry),
			zap.String("scope", scope),
			zap.Error(r.err),
		)

		return scopeState{Databases: prev.Databases, Listed: true, Sticky: prev.Sticky} //nolint:exhaustruct
	}

	p.logger.Warn("no patroni member answered, cluster published without databases",
		zap.String("entry", p.entry),
		zap.String("scope", scope),
		zap.Error(r.err),
	)

	return scopeState{Sticky: prev.Sticky} //nolint:exhaustruct
}

func (p *Provider) reportUnmatchedOverrides(scopes []scopeData) {
	present := make(map[string]bool, len(scopes))
	for _, s := range scopes {
		present[strings.ToLower(s.Name)] = true
	}

	for name := range p.overrides {
		if present[name] || p.noOverride[name] {
			continue
		}

		p.noOverride[name] = true

		p.logger.Debug("override has no matching scope",
			zap.String("entry", p.entry),
			zap.String("scope", name),
		)
	}
}
