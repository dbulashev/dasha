package postgres

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/discovery/filter"
	"github.com/dbulashev/dasha/internal/version"
)

const (
	connectTimeout = 5 * time.Second
	queryTimeout   = 10 * time.Second

	// matchAllClusters satisfies filter.New: this provider serves a single
	// cluster, so only the database patterns matter.
	matchAllClusters = ".*"
)

// Provider discovers the databases of one PostgreSQL cluster.
type Provider struct {
	name   string
	cfg    Config
	filter *filter.Filter
	logger *zap.Logger
	// hostIdx is the host that answered last, so a dead first host is not
	// retried first every cycle. Only the discovery loop touches it, one cycle
	// at a time.
	hostIdx int
	// published is the database list of the previous cycle, kept to log what
	// changed. Same single-goroutine ownership as hostIdx.
	published []string
}

// NewProvider decodes a postgres discovery block. The entry name becomes the
// cluster name.
func NewProvider(name string, raw map[string]any, logger *zap.Logger) (*Provider, error) {
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

	if cfg.PasswordFromEnv != "" && cfg.Password == "" {
		logger.Warn("password_from_env is set but the variable is empty",
			zap.String("entry", name),
			zap.String("variable", cfg.PasswordFromEnv),
		)
	}

	f, err := filter.New(matchAllClusters, cfg.Db, nil, cfg.ExcludeDb)
	if err != nil {
		return nil, fmt.Errorf("compile filter: %w", err)
	}

	// The entry's own settings; the engine logs name, type and interval.
	logger.Info("postgres discovery configured",
		zap.String("entry", name),
		zap.Strings("hosts", cfg.Hosts),
		zap.String("port", cfg.Port),
		zap.String("bootstrap_db", cfg.BootstrapDB),
	)

	return &Provider{
		name:      name,
		cfg:       cfg,
		filter:    f,
		logger:    logger,
		hostIdx:   0,
		published: nil,
	}, nil
}

// Interval returns the configured refresh interval.
func (p *Provider) Interval() time.Duration {
	return p.cfg.interval()
}

// Discover lists the cluster's databases and returns it as a single cluster.
func (p *Provider) Discover(ctx context.Context) ([]config.Cluster, error) {
	names, err := p.listDatabases(ctx)
	if err != nil {
		return nil, err
	}

	return p.clusters(names), nil
}

// clusters applies the db filters and wraps the result into one cluster.
func (p *Provider) clusters(names []string) []config.Cluster {
	databases := make([]config.Database, 0, len(names))
	kept := make([]string, 0, len(names))

	for _, name := range names {
		if p.filter.MatchDb(name) {
			databases = append(databases, config.Database(name))
			kept = append(kept, name)
		}
	}

	p.logChanges(kept)

	if len(databases) == 0 {
		p.logger.Warn("no databases left after filtering",
			zap.String("entry", p.name),
			zap.Int("candidates", len(names)),
		)
	}

	hosts := make([]config.Host, 0, len(p.cfg.Hosts))
	for _, h := range p.cfg.Hosts {
		hosts = append(hosts, config.Host(h))
	}

	return []config.Cluster{{
		Name:       config.ClusterName(p.name),
		UserName:   p.cfg.User,
		Password:   p.cfg.Password,
		Port:       p.cfg.Port,
		Databases:  databases,
		Hosts:      hosts,
		Source:     config.SourcePostgres,
		ProviderID: p.name,
		Labels:     map[string]string{"bootstrap_db": p.cfg.BootstrapDB},
	}}
}

// logChanges reports what this cycle added or dropped, so a CREATE/DROP
// DATABASE leaves a trace in the log and not only in the UI.
func (p *Provider) logChanges(current []string) {
	if p.published == nil {
		p.logger.Info("databases discovered",
			zap.String("entry", p.name),
			zap.Strings("databases", current),
		)

		p.published = current

		return
	}

	added := missingFrom(current, p.published)
	removed := missingFrom(p.published, current)

	if len(added) > 0 || len(removed) > 0 {
		p.logger.Info("database list changed",
			zap.String("entry", p.name),
			zap.Strings("added", added),
			zap.Strings("removed", removed),
		)
	}

	p.published = current
}

// missingFrom returns the names of from that other does not contain.
func missingFrom(from, other []string) []string {
	present := make(map[string]bool, len(other))
	for _, name := range other {
		present[name] = true
	}

	var diff []string

	for _, name := range from {
		if !present[name] {
			diff = append(diff, name)
		}
	}

	return diff
}

// listDatabases queries the first host that answers, starting from the one that
// answered last cycle.
func (p *Provider) listDatabases(ctx context.Context) ([]string, error) {
	var lastErr error

	for i := range p.cfg.Hosts {
		idx := (p.hostIdx + i) % len(p.cfg.Hosts)
		host := p.cfg.Hosts[idx]

		names, err := p.queryDatabases(ctx, host)
		if err != nil {
			lastErr = err

			p.logger.Debug("host did not answer, trying the next one",
				zap.String("entry", p.name),
				zap.String("host", host),
				zap.Error(err),
			)

			continue
		}

		p.hostIdx = idx

		p.logger.Debug("databases listed",
			zap.String("entry", p.name),
			zap.String("host", host),
			zap.Int("databases", len(names)),
		)

		return names, nil
	}

	return nil, fmt.Errorf("no host answered for entry %q: %w", p.name, lastErr)
}

func (p *Provider) queryDatabases(ctx context.Context, host string) ([]string, error) {
	connCfg, err := pgx.ParseConfig(p.dsn(host))
	if err != nil {
		return nil, fmt.Errorf("parse connection config: %w", err)
	}

	connCfg.ConnectTimeout = connectTimeout
	// Simple protocol and a recognisable application_name for the same reason
	// the monitored pools use them: transaction-pooling proxies in front of the
	// cluster, and a DBA looking at pg_stat_activity.
	connCfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	if connCfg.RuntimeParams == nil {
		connCfg.RuntimeParams = make(map[string]string)
	}

	// Same shape as the pools' application_name, with the role spelled out.
	connCfg.RuntimeParams["application_name"] = fmt.Sprintf("dasha discovery %s %s",
		version.GetBuildNumber(), version.GetBuildDate())

	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	conn, err := pgx.ConnectConfig(queryCtx, connCfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	defer func() {
		// Closing on its own context: the query context may already be done.
		_ = conn.Close(context.WithoutCancel(ctx))
	}()

	rows, err := conn.Query(queryCtx, listDatabasesQuery)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}

	defer rows.Close()

	var names []string

	for rows.Next() {
		var name string

		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan datname: %w", err)
		}

		names = append(names, name)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}

	return names, nil
}

// dsn builds the connection string for the bootstrap database. url.URL escapes
// credentials, which a hand-built string would not.
func (p *Provider) dsn(host string) string {
	u := url.URL{ //nolint:exhaustruct
		Scheme: "postgres",
		User:   url.UserPassword(p.cfg.User, p.cfg.Password),
		Host:   net.JoinHostPort(host, p.cfg.Port),
		Path:   "/" + p.cfg.BootstrapDB,
	}

	return u.String()
}
