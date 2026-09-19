package logs

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

// Settings a scan reports on. The auto_explain ones exist only while the
// library is loaded, which is how their presence answers whether it is.
const (
	settingPrefix         = "auto_explain."
	settingLogMinDuration = "auto_explain.log_min_duration"
	settingLogAnalyze     = "auto_explain.log_analyze"
	settingLogFormat      = "auto_explain.log_format"
	settingLogLevel       = "auto_explain.log_level"
	settingComputeQueryID = "compute_query_id"
)

// settingsTimeout bounds the diagnosis: a cluster that does not answer must not
// spend the budget of the log read.
const settingsTimeout = 5 * time.Second

// ClusterSettings reads the PostgreSQL settings a scan reports on. The
// repository implements it; the interface lives here so a cluster Dasha cannot
// reach leaves the diagnosis out of the answer instead of failing the scan.
type ClusterSettings interface {
	GetPlanLogSettings(ctx context.Context, clusterName, instanceName string) (map[string]string, error)
}

// Configuration is what pg_settings says about plan logging on one host.
// AutoExplain false means the library is not loaded and the rest is unset.
type Configuration struct {
	Instance         string
	AutoExplain      bool
	LogMinDurationMs *int
	LogAnalyze       *bool
	LogFormat        string
	LogLevel         string
	ComputeQueryID   string
}

// planLogging is what the cluster answered about plan logging: the diagnosis of
// one host, and the levels auto_explain writes plans with on every host the
// scan covers. Levels is empty unless every one of them answered.
type planLogging struct {
	Config *Configuration
	Levels []string
}

// readSettings reads plan-logging settings off the given hosts. A host that
// does not answer is left out; the second value says whether all of them did.
func (s *service) readSettings(ctx context.Context, cluster config.Cluster, hosts []string) ([]*Configuration, bool) {
	if s.settings == nil || len(hosts) == 0 {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(ctx, settingsTimeout)
	defer cancel()

	out := make([]*Configuration, 0, len(hosts))

	for _, host := range hosts {
		values, err := s.settings.GetPlanLogSettings(ctx, cluster.Name.String(), host)
		if err != nil {
			s.logger.Warn("log insights: plan logging settings unavailable",
				zap.String("cluster", cluster.Name.String()),
				zap.String("instance", host),
				zap.Error(err))

			continue
		}

		cfg := newConfiguration(values)
		cfg.Instance = host

		out = append(out, cfg)
	}

	return out, len(out) == len(hosts)
}

// configuration is the diagnosis alone, off the host a scan was asked for or
// the first of the cluster. An unreachable cluster leaves it nil and the scan
// runs regardless.
func (s *service) configuration(ctx context.Context, cluster config.Cluster, host string) *Configuration {
	cfgs, _ := s.readSettings(ctx, cluster, diagnosisHost(cluster, host))
	if len(cfgs) == 0 {
		return nil
	}

	return cfgs[0]
}

// configurationAsync reads the diagnosis while the caller scans: a scan that
// does not narrow by it must not wait for the cluster before reading the log.
func (s *service) configurationAsync(
	ctx context.Context,
	cluster config.Cluster,
	host string,
) func() *Configuration {
	done := make(chan *Configuration, 1)

	go func() { done <- s.configuration(ctx, cluster, host) }()

	return func() *Configuration { return <-done }
}

// planLogging adds the levels of every host the scan covers to the diagnosis.
func (s *service) planLogging(ctx context.Context, cluster config.Cluster, host string) planLogging {
	cfgs, complete := s.readSettings(ctx, cluster, scanHosts(cluster, host))

	out := planLogging{} //nolint:exhaustruct
	if len(cfgs) == 0 {
		return out
	}

	out.Config = cfgs[0]

	if !complete {
		return out
	}

	for _, cfg := range cfgs {
		if cfg.LogLevel != "" && !slices.Contains(out.Levels, cfg.LogLevel) {
			out.Levels = append(out.Levels, cfg.LogLevel)
		}
	}

	return out
}

func diagnosisHost(cluster config.Cluster, host string) []string {
	hosts := scanHosts(cluster, host)
	if len(hosts) == 0 {
		return nil
	}

	return hosts[:1]
}

// scanHosts are the hosts a scan reads, which is the whole cluster unless one
// was asked for.
func scanHosts(cluster config.Cluster, host string) []string {
	if host != "" {
		return []string{host}
	}

	out := make([]string, 0, len(cluster.Hosts))
	for _, h := range cluster.Hosts {
		out = append(out, h.String())
	}

	return out
}

func newConfiguration(values map[string]string) *Configuration {
	cfg := &Configuration{ //nolint:exhaustruct
		ComputeQueryID: values[settingComputeQueryID],
	}

	for name := range values {
		if strings.HasPrefix(name, settingPrefix) {
			cfg.AutoExplain = true

			break
		}
	}

	if !cfg.AutoExplain {
		return cfg
	}

	if ms, err := strconv.Atoi(values[settingLogMinDuration]); err == nil {
		cfg.LogMinDurationMs = &ms
	}

	if on, ok := parseBoolSetting(values[settingLogAnalyze]); ok {
		cfg.LogAnalyze = &on
	}

	cfg.LogFormat = values[settingLogFormat]
	cfg.LogLevel = values[settingLogLevel]

	return cfg
}

func parseBoolSetting(v string) (bool, bool) {
	switch v {
	case "on":
		return true, true
	case "off":
		return false, true
	default:
		return false, false
	}
}

// planSeverities are the severities auto_explain writes plans with, in the
// spelling the source stores them. A level the source does not accept narrows
// nothing: dropping it would hide the records of the host that logs at it.
func planSeverities(levels []string, fm source.FieldMap) []string {
	if len(levels) == 0 {
		return nil
	}

	out := make([]string, 0, len(levels))

	for _, level := range levels {
		v, ok := fm.CanonicalSeverity(level)
		if !ok {
			return nil
		}

		out = append(out, v)
	}

	return out
}
