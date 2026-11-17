package piicatalog

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/dto"
)

type Repo interface {
	GetInstanceInfo(ctx context.Context, clusterName, instanceName string) (dto.InstanceInfo, error)
	GetColumnCatalog(
		ctx context.Context, clusterName, instanceName, databaseName string,
		excludeSchemas []string, stmtTimeout time.Duration,
	) ([]Pair, error)
}

const (
	// Connection setup and server-version lookup on top of statement_timeout.
	attemptSlack = 10 * time.Second
	probeTimeout = 10 * time.Second

	maxErrorLen = 1024
)

type Collector struct {
	repo           Repo
	excludeSchemas []string
	stmtTimeout    time.Duration
	logger         *zap.Logger
}

func NewCollector(repo Repo, excludeSchemas []string, stmtTimeout time.Duration, logger *zap.Logger) *Collector {
	if excludeSchemas == nil {
		// A nil slice binds as NULL, and NOT (x LIKE ANY(NULL)) excludes everything.
		excludeSchemas = []string{}
	}

	return &Collector{repo: repo, excludeSchemas: excludeSchemas, stmtTimeout: stmtTimeout, logger: logger}
}

type host struct {
	name    string
	version int
}

// orderHosts puts replicas first (config order), then primaries, then hosts
// whose role could not be determined.
func (c *Collector) orderHosts(ctx context.Context, cl config.Cluster) []host {
	var replicas, primaries, unknown []host

	for _, h := range cl.Hosts {
		pctx, cancel := context.WithTimeout(ctx, probeTimeout)
		info, err := c.repo.GetInstanceInfo(pctx, string(cl.Name), string(h))

		cancel()

		switch {
		case err != nil:
			c.logger.Debug("pii_catalog: host role unknown",
				zap.String("cluster", string(cl.Name)), zap.String("host", string(h)), zap.Error(err))

			unknown = append(unknown, host{name: string(h)})
		case info.InRecovery:
			replicas = append(replicas, host{name: string(h), version: info.VersionNum})
		default:
			primaries = append(primaries, host{name: string(h), version: info.VersionNum})
		}
	}

	return append(append(replicas, primaries...), unknown...)
}

// collect tries hosts in order and returns the first successful snapshot.
func (c *Collector) collect(
	ctx context.Context, cluster string, hosts []host, database string, attemptAt time.Time,
) (Snapshot, error) {
	if len(hosts) == 0 {
		return Snapshot{}, fmt.Errorf("cluster has no hosts")
	}

	errs := make([]string, 0, len(hosts))

	for _, h := range hosts {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err.Error())

			break
		}

		actx, cancel := context.WithTimeout(ctx, c.stmtTimeout+attemptSlack)
		pairs, err := c.repo.GetColumnCatalog(actx, cluster, h.name, database, c.excludeSchemas, c.stmtTimeout)

		cancel()

		if err == nil {
			return NewSnapshot(pairs, h.name, h.version, attemptAt), nil
		}

		errs = append(errs, h.name+": "+err.Error())
	}

	return Snapshot{}, fmt.Errorf("%s", truncate(strings.Join(errs, "; "), maxErrorLen))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}

	return s
}
