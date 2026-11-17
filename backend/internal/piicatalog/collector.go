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
	// Also returns server_version_num of the instance that answered.
	GetColumnCatalog(
		ctx context.Context, clusterName, instanceName, databaseName string,
		excludeSchemas []string, stmtTimeout time.Duration,
	) ([]Pair, int, error)
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
		pairs, version, err := c.repo.GetColumnCatalog(actx, cluster, h, database, c.excludeSchemas, c.stmtTimeout)

		cancel()

		if err == nil {
			return NewSnapshot(pairs, h, version, attemptAt), nil
		}

		errs = append(errs, h+": "+err.Error())
	}

	return Snapshot{}, fmt.Errorf("%s", truncate(strings.Join(errs, "; "), maxErrorLen))
}

// truncate also replaces invalid UTF-8: a server may report a startup error in
// its lc_messages encoding, which a text column rejects.
func truncate(s string, n int) string {
	s = strings.ToValidUTF8(s, "?")
	if len(s) <= n {
		return s
	}

	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}

	return s
}
