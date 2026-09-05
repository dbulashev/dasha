// Package yandexmdb adapts Yandex Managed Databases log streaming to the log
// source contract.
package yandexmdb

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/discovery/yandex"
	"github.com/dbulashev/dasha/internal/logs/source"
)

// Name is the reserved source name of the implicit Yandex MDB binding.
const Name = config.SourceYandexMDB

// checkWindow is the range Check reads a sample record from.
const checkWindow = time.Hour

// checkProbeLimit caps the records Check counts: the probe answers whether the
// stream carries data, and the API reports the count as seen in the window.
const checkProbeLimit = 1000

// hostField carries the host name Yandex adds to every record of both streams.
const hostField = "hostname"

// Provider reads logs through the Yandex Cloud MDB API.
type Provider struct {
	registry *yandex.Registry
}

// New builds a provider over the SDK registry the discovery engine fills.
func New(registry *yandex.Registry) *Provider {
	return &Provider{registry: registry}
}

// Matches claims clusters discovered from Yandex MDB that carry the ids the
// log API needs.
func (p *Provider) Matches(cluster config.Cluster) bool {
	return cluster.Source == config.SourceYandexMDB &&
		cluster.ProviderID != "" &&
		cluster.Labels["folder_id"] != ""
}

func (p *Provider) Streams() []string {
	return []string{source.StreamPostgreSQL, source.StreamPooler}
}

func (p *Provider) Fields(stream string) source.FieldMap {
	var preset string

	switch stream {
	case source.StreamPostgreSQL:
		preset = source.PresetCSVLog
	case source.StreamPooler:
		preset = source.PresetOdyssey
	default:
		return source.FieldMap{}
	}

	fm, ok := source.Preset(preset)
	if !ok {
		return source.FieldMap{}
	}

	fm.Host = hostField

	return fm
}

func (p *Provider) Stream(ctx context.Context, sp source.StreamParams, fn func(source.Record) bool) error {
	sdk, params, err := p.params(sp)
	if err != nil {
		return err
	}

	err = sdk.StreamLogs(ctx, params, func(rec yandex.LogRecord) bool {
		return fn(source.Record{
			Timestamp: rec.Timestamp,
			Fields:    rec.Fields,
			Token:     rec.Token,
		})
	})
	if err != nil {
		return fmt.Errorf("yandexmdb stream: %w", err)
	}

	return nil
}

// Check counts the records of the last hour up to the probe limit and reports
// which mapped fields the first of them carries.
func (p *Provider) Check(ctx context.Context, cluster config.Cluster, stream string) (source.CheckResult, error) {
	now := time.Now()

	sdk, params, err := p.params(source.StreamParams{
		Cluster: cluster,
		Stream:  stream,
		From:    now.Add(-checkWindow),
		To:      now,
		Filter:  source.Filter{},
		Token:   "",
	})
	if err != nil {
		return source.CheckResult{}, err
	}

	res := source.CheckResult{
		Target:    cluster.ProviderID,
		Documents: 0,
		Found:     map[string]string{},
		Missing:   nil,
		Types:     nil,
		Sample:    nil,
	}

	err = sdk.StreamLogs(ctx, params, func(rec yandex.LogRecord) bool {
		res.Documents++

		if res.Sample == nil {
			res.Sample = rec.Fields
		}

		return res.Documents < checkProbeLimit
	})
	if err != nil {
		return source.CheckResult{}, fmt.Errorf("yandexmdb check: %w", err)
	}

	// Without a record there is nothing to inspect: an empty window says
	// nothing about the mapping.
	if res.Sample == nil {
		return res, nil
	}

	for role, field := range p.Fields(stream).Roles() {
		if _, ok := res.Sample[field]; ok {
			res.Found[role] = field

			continue
		}

		res.Missing = append(res.Missing, role)
	}

	slices.Sort(res.Missing)

	return res, nil
}

func (p *Provider) params(sp source.StreamParams) (*yandex.SDK, yandex.StreamLogsParams, error) {
	st, ok := serviceType(sp.Stream)
	if !ok {
		return nil, yandex.StreamLogsParams{}, fmt.Errorf("%w: %q", source.ErrStream, sp.Stream)
	}

	if !p.Matches(sp.Cluster) {
		return nil, yandex.StreamLogsParams{}, fmt.Errorf("%w: cluster is not a Yandex MDB cluster", source.ErrUnavailable)
	}

	sdk, ok := p.registry.Get(sp.Cluster.Labels["folder_id"])
	if !ok {
		return nil, yandex.StreamLogsParams{}, fmt.Errorf("%w: no SDK for folder", source.ErrUnavailable)
	}

	return sdk, yandex.StreamLogsParams{
		ClusterID:   sp.Cluster.ProviderID,
		ServiceType: st,
		From:        sp.From,
		To:          sp.To,
		Filter:      buildFilter(p.Fields(sp.Stream), sp.Filter),
		Columns:     nil,
		RecordToken: sp.Token,
	}, nil
}

func serviceType(stream string) (yandex.ServiceType, bool) {
	switch stream {
	case source.StreamPostgreSQL:
		return yandex.ServicePostgreSQL, true
	case source.StreamPooler:
		return yandex.ServicePooler, true
	default:
		return yandex.ServicePostgreSQL, false
	}
}

// buildFilter assembles the native StreamLogs expression from allowlisted
// values only (canonical severities and a validated host), so it is
// injection-safe.
func buildFilter(fm source.FieldMap, f source.Filter) string {
	var parts []string

	if len(f.Severities) > 0 {
		quoted := make([]string, len(f.Severities))
		for i, sev := range f.Severities {
			quoted[i] = `"` + sev + `"`
		}

		parts = append(parts, fmt.Sprintf("message.%s IN (%s)", fm.Severity, strings.Join(quoted, ", ")))
	}

	if f.Host != "" {
		parts = append(parts, fmt.Sprintf(`message.%s = "%s"`, fm.Host, f.Host))
	}

	return strings.Join(parts, " AND ")
}
