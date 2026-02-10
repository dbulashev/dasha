package yandex

import (
	"context"
	"fmt"

	"github.com/dbulashev/dasha/internal/discovery/filter"

	"github.com/yandex-cloud/go-genproto/yandex/cloud/mdb/postgresql/v1"
	ycsdk "github.com/yandex-cloud/go-sdk"
	"go.uber.org/zap"
)

// YandexMDBPort is the default connection port for Yandex Managed PostgreSQL.
const YandexMDBPort = "6432"

// Database represents a discovered PostgreSQL database.
type Database struct {
	Name  string
	Owner string
}

// Host represents a discovered PostgreSQL host.
type Host struct {
	Name   string
	ZoneID string
	Role   postgresql.Host_Role
	Health postgresql.Host_Health
}

// Cluster represents a discovered Yandex MDB PostgreSQL cluster.
type Cluster struct {
	ID        string
	Name      string
	FolderID  string
	Hosts     []Host
	Databases []Database
}

// GetPostgreSQLClusters returns a filtered list of clusters from the Yandex Cloud API.
func (sdk *SDK) GetPostgreSQLClusters(
	ctx context.Context,
	folderID string,
	filters []filter.Filter,
	logger *zap.Logger,
) ([]Cluster, error) {
	client, err := sdk.Client(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetPostgreSQLClusters | %w", err)
	}

	ctxOp, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		clusters []Cluster
		req      postgresql.ListClustersRequest
	)

	req.FolderId = folderID

	for {
		resp, err := client.MDB().PostgreSQL().Cluster().List(ctxOp, &req)
		if err != nil {
			return nil, fmt.Errorf("GetPostgreSQLClusters | list clusters: %w", err)
		}

		for _, cluster := range resp.Clusters {
			if cluster.Status != postgresql.Cluster_RUNNING && cluster.Status != postgresql.Cluster_UPDATING {
				logger.Debug("cluster not running, skipped", zap.String("cluster", cluster.Name))

				continue
			}

			// Find which filters match this cluster name.
			matched := make([]int, 0)

			for i, f := range filters {
				if f.MatchName(cluster.Name) {
					matched = append(matched, i)
				}
			}

			if len(matched) == 0 {
				logger.Debug("cluster not matched by filters, skipped", zap.String("cluster", cluster.Name))

				continue
			}

			hosts := listAliveHosts(ctxOp, client, cluster.Id, logger)

			databases, err := listMatchedDatabases(ctxOp, client, cluster.Id, filters, matched, logger)
			if err != nil {
				return nil, fmt.Errorf("GetPostgreSQLClusters | %w", err)
			}

			if len(databases) == 0 {
				logger.Debug("no matching databases in cluster", zap.String("cluster", cluster.Name))

				continue
			}

			logger.Debug("discovered cluster",
				zap.String("cluster", cluster.Name),
				zap.Int("hosts", len(hosts)),
				zap.Int("databases", len(databases)),
			)

			clusters = append(clusters, Cluster{
				ID:        cluster.Id,
				Name:      cluster.Name,
				FolderID:  cluster.FolderId,
				Hosts:     hosts,
				Databases: databases,
			})
		}

		if resp.NextPageToken == "" {
			break
		}

		req.PageToken = resp.NextPageToken
	}

	return clusters, nil
}

func listAliveHosts(ctx context.Context, client *ycsdk.SDK, clusterID string, logger *zap.Logger) []Host {
	var hosts []Host

	iter := client.MDB().PostgreSQL().Cluster().ClusterHostsIterator(ctx,
		&postgresql.ListClusterHostsRequest{ClusterId: clusterID}) //nolint:exhaustruct
	for iter.Next() {
		h := iter.Value()
		if h.Health != postgresql.Host_ALIVE {
			continue
		}

		hosts = append(hosts, Host{
			Name:   h.Name,
			ZoneID: h.ZoneId,
			Role:   h.Role,
			Health: h.Health,
		})
	}

	if err := iter.Error(); err != nil {
		logger.Warn("failed to list hosts", zap.String("cluster_id", clusterID), zap.Error(err))
	}

	return hosts
}

func listMatchedDatabases(
	ctx context.Context,
	client *ycsdk.SDK,
	clusterID string,
	filters []filter.Filter,
	matched []int, _ *zap.Logger,
) ([]Database, error) {
	var (
		databases []Database
		req       postgresql.ListDatabasesRequest
	)

	req.ClusterId = clusterID

	for {
		resp, err := client.MDB().PostgreSQL().Database().List(ctx, &req)
		if err != nil {
			return nil, fmt.Errorf("listMatchedDatabases | cluster %s: %w", clusterID, err)
		}

		for _, db := range resp.Databases {
			skip := true

			for _, idx := range matched {
				if filters[idx].MatchDb(db.Name) {
					skip = false

					break
				}
			}

			if !skip {
				databases = append(databases, Database{
					Name:  db.Name,
					Owner: db.Owner,
				})
			}
		}

		if resp.NextPageToken == "" {
			break
		}

		req.PageToken = resp.NextPageToken
	}

	return databases, nil
}
