package piicatalog

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/pkg/sanitize"
)

type Repo interface {
	GetInstanceInfo(ctx context.Context, clusterName, instanceName string) (dto.InstanceInfo, error)
	// Also returns server_version_num of the instance that answered.
	GetColumnCatalog(
		ctx context.Context, clusterName, instanceName, databaseName string,
		excludeSchemas []string, stmtTimeout time.Duration,
	) ([]Column, int, error)
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
	return &Collector{repo: repo, excludeSchemas: excludeSchemas, stmtTimeout: stmtTimeout, logger: logger}
}

// orderHosts puts replicas first (config order), then primaries, then hosts
// whose role could not be determined.
func (c *Collector) orderHosts(ctx context.Context, cl config.Cluster) []string {
	var replicas, primaries, unknown []string

	for _, h := range cl.Hosts {
		pctx, cancel := context.WithTimeout(ctx, probeTimeout)
		info, err := c.repo.GetInstanceInfo(pctx, string(cl.Name), string(h))

		cancel()

		switch {
		case err != nil:
			c.logger.Debug("pii_catalog: host role unknown",
				zap.String("cluster", string(cl.Name)), zap.String("host", string(h)), zap.Error(err))

			unknown = append(unknown, string(h))
		case info.InRecovery:
			replicas = append(replicas, string(h))
		default:
			primaries = append(primaries, string(h))
		}
	}

	return append(append(replicas, primaries...), unknown...)
}

// collect tries hosts in order and returns the first successful snapshot.
func (c *Collector) collect(
	ctx context.Context, cluster string, hosts []string, database string, attemptAt time.Time,
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
		cols, version, err := c.repo.GetColumnCatalog(actx, cluster, h, database, c.excludeSchemas, c.stmtTimeout)

		cancel()

		if err == nil {
			return NewSnapshot(cols, h, version, attemptAt), nil
		}

		errs = append(errs, h+": "+err.Error())
	}

	// A server may report a startup error in its lc_messages encoding, which a
	// text column rejects.
	return Snapshot{}, fmt.Errorf("%s", sanitize.TruncateUTF8(strings.Join(errs, "; "), maxErrorLen))
}
