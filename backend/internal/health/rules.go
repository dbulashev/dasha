package health

import "sort"

// Severity is the importance of a triggered rule.
type Severity string

const (
	SeverityHigh   Severity = "HIGH"
	SeverityMedium Severity = "MEDIUM"
	SeverityLow    Severity = "LOW"
)

// severityRank lets us sort severities deterministically (HIGH first).
func severityRank(s Severity) int {
	switch s {
	case SeverityHigh:
		return 0
	case SeverityMedium:
		return 1
	case SeverityLow:
		return 2
	default:
		return 3
	}
}

// Hit is the runtime trigger output for one rule.
type Hit struct {
	Severity    Severity
	MetricValue float64
	Context     map[string]any
}

// Rule defines an automatic diagnosis check. The Evaluate function inspects raw
// metrics and returns a Hit with the resulting severity, or nil when the rule
// has not triggered.
//
// Texts (title, short description, instructions, SQL) are not stored here;
// they live in the frontend i18n bundle under
// healthScore.recommendations.<id>.*, keyed by ID.
type Rule struct {
	ID           string
	Category     string
	RelatedRoute string
	Evaluate     func(RawMetrics) *Hit
}

// Recommendation is one rule's evaluation result, ready to ship over the API.
type Recommendation struct {
	RuleID       string         `json:"rule_id"`
	Category     string         `json:"category"`
	Severity     Severity       `json:"severity"`
	MetricValue  float64        `json:"metric_value"`
	Context      map[string]any `json:"context,omitempty"`
	RelatedRoute string         `json:"related_route,omitempty"`
}

// instanceOnlyCategories lists categories that have no meaning at the
// per-database drilldown level. Used to filter rules when database != "".
var instanceOnlyCategories = map[string]bool{
	"connections":    true,
	"replication":    true,
	"horizon":        true,
	"wal_checkpoint": true,
	"locks":          true,
}

// Evaluate runs all rules against the given metrics and returns triggered
// recommendations, sorted by severity (HIGH first) and then by rule ID for
// stable output.
//
// If databaseScoped is true, instance-only categories (connections, replication,
// horizon, wal_checkpoint, locks) are skipped — they have no meaning at the
// per-database level.
//
// When m.InRecovery is true, all maintenance rules are skipped — standbys
// cannot run autovacuum/ANALYZE, so the metrics reflect the primary state and
// any action belongs there. Mirrors the maintenance-weight drop in
// CalculateWithWeights.
func Evaluate(m RawMetrics, databaseScoped bool) []Recommendation {
	out := make([]Recommendation, 0, len(Registry))

	for _, r := range Registry {
		if databaseScoped && instanceOnlyCategories[r.Category] {
			continue
		}

		if m.InRecovery && r.Category == "maintenance" {
			continue
		}

		hit := r.Evaluate(m)
		if hit == nil {
			continue
		}

		out = append(out, Recommendation{
			RuleID:       r.ID,
			Category:     r.Category,
			Severity:     hit.Severity,
			MetricValue:  hit.MetricValue,
			Context:      hit.Context,
			RelatedRoute: r.RelatedRoute,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if a, b := severityRank(out[i].Severity), severityRank(out[j].Severity); a != b {
			return a < b
		}

		return out[i].RuleID < out[j].RuleID
	})

	return out
}

// Registry is the catalog of all rules, in declaration order.
// Severity thresholds match the design caterogy table.
var Registry = []Rule{
	{
		ID: "high_connection_ratio", Category: "connections", RelatedRoute: "/queries",
		Evaluate: func(m RawMetrics) *Hit {
			if m.MaxConnections == 0 {
				return nil
			}

			ratio := float64(m.TotalConnections) / float64(m.MaxConnections)

			sev := severityForRatio(ratio, 0.95, 0.80, 0.60)
			if sev == "" {
				return nil
			}

			return &Hit{
				Severity:    sev,
				MetricValue: ratio,
				Context: map[string]any{
					"total":           m.TotalConnections,
					"max_connections": m.MaxConnections,
				},
			}
		},
	},
	{
		ID: "idle_in_transaction", Category: "connections", RelatedRoute: "/queries",
		Evaluate: func(m RawMetrics) *Hit {
			// Loose thresholds: on busy OLTP 1 idle in tx >30s is a regular blip,
			// not a sustained issue. HIGH 10 catches real connection-leak storms.
			sev := severityForCount(m.IdleInTransaction, 10, 5, 2)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: float64(m.IdleInTransaction)}
		},
	},
	{
		ID: "long_running_transaction", Category: "connections", RelatedRoute: "/queries",
		Evaluate: func(m RawMetrics) *Hit {
			sec := m.LongestTransactionSeconds

			sev := severityForRatio(sec, 1800, 600, 300)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: sec}
		},
	},
	{
		ID: "low_cache_hit_ratio", Category: "performance", RelatedRoute: "/indexes-usage",
		Evaluate: func(m RawMetrics) *Hit {
			r := m.CacheHitRatio

			// Relaxed thresholds: classic 99/95/90 over-triggers on OLAP
			// workloads with large sequential scans (cold cache is normal).
			// OLTP users who want strict scoring can raise the Performance weight.
			var sev Severity

			switch {
			case r < 85:
				sev = SeverityHigh
			case r < 90:
				sev = SeverityMedium
			case r < 95:
				sev = SeverityLow
			default:
				return nil
			}

			return &Hit{Severity: sev, MetricValue: r}
		},
	},
	{
		ID: "high_max_dead_ratio", Category: "storage", RelatedRoute: "/tables",
		Evaluate: func(m RawMetrics) *Hit {
			// HIGH lowered from 50% to 30%: on OLTP with a working autovacuum
			// bloat rarely passes 5%, so 30% is already a serious deviation.
			sev := severityForRatio(m.MaxDeadRatio, 30, 20, 10)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: m.MaxDeadRatio}
		},
	},
	{
		ID: "high_avg_dead_ratio", Category: "storage", RelatedRoute: "/tables",
		Evaluate: func(m RawMetrics) *Hit {
			// MED raised from 10% to 15%: 5-15% average dead ratio is normal
			// background for active OLTP.
			sev := severityForRatio(m.AvgDeadRatio, 25, 15, 5)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: m.AvgDeadRatio}
		},
	},
	{
		ID: "many_bloated_tables", Category: "storage", RelatedRoute: "/tables",
		Evaluate: func(m RawMetrics) *Hit {
			sev := severityForCount(m.TablesHighBloat, 20, 10, 5)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: float64(m.TablesHighBloat)}
		},
	},
	{
		ID: "replication_lag_time", Category: "replication", RelatedRoute: "/replication",
		Evaluate: func(m RawMetrics) *Hit {
			if m.ReplicaCount == 0 {
				return nil
			}

			sev := severityForRatio(m.MaxReplayLagSeconds, 30, 5, 1)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: m.MaxReplayLagSeconds}
		},
	},
	{
		ID: "replication_lag_bytes", Category: "replication", RelatedRoute: "/replication",
		Evaluate: func(m RawMetrics) *Hit {
			if m.ReplicaCount == 0 {
				return nil
			}

			mb := float64(m.MaxLagBytes) / (1024 * 1024)

			sev := severityForRatio(mb, 1024, 100, 10)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: float64(m.MaxLagBytes)}
		},
	},
	{
		ID: "disconnected_replicas", Category: "replication", RelatedRoute: "/replication",
		Evaluate: func(m RawMetrics) *Hit {
			if m.DisconnectedReplicas == 0 {
				return nil
			}

			sev := SeverityMedium
			if m.DisconnectedReplicas >= 2 {
				sev = SeverityHigh
			}

			return &Hit{Severity: sev, MetricValue: float64(m.DisconnectedReplicas)}
		},
	},
	{
		// Thresholds aligned with PG mechanics (see "PostgreSQL Internals" §7.3):
		// 150M = vacuum_freeze_table_age (aggressive freeze starts here),
		// 200M = autovacuum_freeze_max_age (emergency autovacuum kicks in),
		// 1.6B = vacuum_failsafe_age (failsafe mode skips index cleanup).
		ID: "xid_wraparound_risk", Category: "maintenance", RelatedRoute: "/maintenance",
		Evaluate: func(m RawMetrics) *Hit {
			age := float64(m.MaxXidAge)

			sev := severityForRatio(age, 1_600_000_000, 200_000_000, 150_000_000)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: age}
		},
	},
	{
		ID: "stale_vacuum", Category: "maintenance", RelatedRoute: "/maintenance",
		Evaluate: func(m RawMetrics) *Hit {
			days := m.MaxVacuumAgeHours / 24

			// Raised thresholds (was 2/7/14): read-mostly tables legitimately
			// go months without vacuum. 7/21/60 days catches real autovacuum
			// stalls without false-positives on low-churn workloads.
			sev := severityForRatio(days, 60, 21, 7)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: m.MaxVacuumAgeHours}
		},
	},
	{
		ID: "tables_never_vacuumed", Category: "maintenance", RelatedRoute: "/maintenance",
		Evaluate: func(m RawMetrics) *Hit {
			sev := severityForCount(m.TablesNeverVacuumed, 5, 2, 1)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: float64(m.TablesNeverVacuumed)}
		},
	},
	{
		// Without autovacuum, dead tuples accumulate indefinitely. Critical.
		ID: "autovacuum_disabled", Category: "maintenance", RelatedRoute: "/maintenance",
		Evaluate: func(m RawMetrics) *Hit {
			if m.AutovacuumEnabled {
				return nil
			}

			return &Hit{Severity: SeverityHigh, MetricValue: 0}
		},
	},
	{
		// Without track_counts autovacuum can't decide which tables to clean — even if enabled.
		ID: "track_counts_disabled", Category: "maintenance", RelatedRoute: "/maintenance",
		Evaluate: func(m RawMetrics) *Hit {
			if m.TrackCountsEnabled {
				return nil
			}

			return &Hit{Severity: SeverityHigh, MetricValue: 0}
		},
	},
	{
		// Disabled autovacuum on individual tables is suspicious; usually unintentional.
		ID: "tables_with_autovacuum_off", Category: "maintenance", RelatedRoute: "/maintenance",
		Evaluate: func(m RawMetrics) *Hit {
			if m.TablesWithAutovacuumOff <= 0 {
				return nil
			}

			return &Hit{Severity: SeverityLow, MetricValue: float64(m.TablesWithAutovacuumOff)}
		},
	},
	{
		// Per-table relfrozenxid age; uses the same thresholds as xid_wraparound_risk
		// because the underlying PG mechanics are identical.
		ID: "relfrozenxid_age_outlier", Category: "maintenance", RelatedRoute: "/maintenance",
		Evaluate: func(m RawMetrics) *Hit {
			age := float64(m.MaxRelfrozenxidAge)

			sev := severityForRatio(age, 1_600_000_000, 200_000_000, 150_000_000)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: age}
		},
	},
	{
		// Horizon held by a long-running transaction prevents vacuum from cleaning
		// dead versions even on healthy tables (see "PostgreSQL Internals" §4.5, §6.2).
		ID: "horizon_lag_xids", Category: "horizon", RelatedRoute: "/queries",
		Evaluate: func(m RawMetrics) *Hit {
			lag := float64(m.HorizonLagXids)

			sev := severityForRatio(lag, 100_000_000, 10_000_000, 1_000_000)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: lag}
		},
	},
	{
		// Frequent requested checkpoints indicate max_wal_size is too small or a load spike.
		// Needs minimum sample to avoid noise on freshly-started instances.
		ID: "requested_checkpoint_ratio", Category: "wal_checkpoint", RelatedRoute: "/maintenance",
		Evaluate: func(m RawMetrics) *Hit {
			total := m.TimedCheckpoints + m.RequestedCheckpoints
			if total < 10 {
				return nil
			}

			ratio := float64(m.RequestedCheckpoints) / float64(total)

			// HIGH lowered from 30% to 20%: 20% of checkpoints being unplanned
			// already indicates max_wal_size is undersized for current write rate.
			sev := severityForRatio(ratio, 0.20, 0.10, 0.05)
			if sev == "" {
				return nil
			}

			return &Hit{
				Severity:    sev,
				MetricValue: ratio,
				Context: map[string]any{
					"timed":     m.TimedCheckpoints,
					"requested": m.RequestedCheckpoints,
				},
			}
		},
	},
	{
		// track_io_timing exposes per-block IO timings in EXPLAIN ANALYZE
		// and pg_stat_statements. Recommended on; minimal overhead on modern OS.
		ID: "track_io_timing_disabled", Category: "performance", RelatedRoute: "/settings",
		Evaluate: func(m RawMetrics) *Hit {
			if m.TrackIoTimingEnabled {
				return nil
			}

			return &Hit{Severity: SeverityLow, MetricValue: 0}
		},
	},

	// === Locks (P2) ===
	{
		// Number of backends actively blocked on a heavyweight lock right now.
		// Snapshot-level — high values indicate active contention.
		ID: "active_lock_waiters", Category: "locks", RelatedRoute: "/locks",
		Evaluate: func(m RawMetrics) *Hit {
			// LOW lowered to 1: on a healthy DB lock-waiters are 0 almost
			// always. A single waiter is already worth a heads-up.
			sev := severityForCount(m.ActiveLockWaiters, 10, 3, 1)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: float64(m.ActiveLockWaiters)}
		},
	},
	{
		// Longest time a process has been waiting on a lock. Sustained high
		// values suggest blocking transactions to investigate via pg_blocking_pids.
		ID: "longest_lock_wait_seconds", Category: "locks", RelatedRoute: "/locks",
		Evaluate: func(m RawMetrics) *Hit {
			sec := m.LongestLockWaitSeconds

			sev := severityForRatio(sec, 60, 30, 10)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: sec}
		},
	},
	{
		// pg_locks rows in `granted = false` state — backends waiting in the queue.
		// Different from active_lock_waiters: counts each ungranted lock, not just
		// blocked processes (one process may wait for multiple locks).
		ID: "ungranted_locks", Category: "locks", RelatedRoute: "/locks",
		Evaluate: func(m RawMetrics) *Hit {
			// Tightened (was 3/10/20): healthy OLTP keeps ungranted = 0 almost
			// always; even 2 waiting locks is worth surfacing.
			sev := severityForCount(m.UngrantedLocks, 15, 5, 2)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: float64(m.UngrantedLocks)}
		},
	},
	{
		// Cumulative deadlocks counter from pg_stat_database since last reset.
		// Any deadlock is a sign of application-level locking order issues.
		// Without history we can't compute a rate — context.stats_reset helps
		// the user interpret the value.
		ID: "deadlocks_rate", Category: "locks", RelatedRoute: "/locks",
		Evaluate: func(m RawMetrics) *Hit {
			// Absolute counter since pg_stat_database_reset: without per-day
			// normalisation any threshold above 0 is uptime-dependent
			// (fresh clusters look clean, long-running ones look catastrophic).
			// Surface as LOW only — «есть deadlocks, посмотри логи»; absolute
			// MED/HIGH gradations would be misleading without history.
			if m.DeadlocksTotal <= 0 {
				return nil
			}

			return &Hit{Severity: SeverityLow, MetricValue: float64(m.DeadlocksTotal)}
		},
	},
	{
		// Heavy lock pool saturation: total pg_locks rows vs the configured
		// capacity (max_locks_per_transaction × max_connections). Approaching
		// the limit risks "out of shared memory" errors on lock acquisition.
		ID: "lock_pool_saturation", Category: "locks", RelatedRoute: "/locks",
		Evaluate: func(m RawMetrics) *Hit {
			if m.MaxLocksPerTransaction <= 0 || m.MaxConnections <= 0 {
				return nil
			}

			capacity := float64(m.MaxLocksPerTransaction) * float64(m.MaxConnections)
			if capacity <= 0 {
				return nil
			}

			ratio := float64(m.HeavyweightLocksTotal) / capacity

			sev := severityForRatio(ratio, 0.8, 0.6, 0.5)
			if sev == "" {
				return nil
			}

			return &Hit{
				Severity:    sev,
				MetricValue: ratio,
				Context: map[string]any{
					"total":    m.HeavyweightLocksTotal,
					"capacity": int64(capacity),
				},
			}
		},
	},

	// === Minor (P3) ===
	{
		// Low HOT-update ratio means most UPDATEs allocate new tuples that
		// require updating all indexes — leading to index bloat and extra
		// dead versions. Healthy OLTP usually keeps HOT ratio above 0.8.
		// Inverted severity: lower ratio = worse.
		ID: "low_hot_update_ratio", Category: "storage", RelatedRoute: "/tables",
		Evaluate: func(m RawMetrics) *Hit {
			r := m.HotUpdateRatio

			// HIGH raised from <30% to <50%: below half-HOT means most UPDATEs
			// allocate fresh tuples and update every index — significant bloat.
			var sev Severity

			switch {
			case r < 0.50:
				sev = SeverityHigh
			case r < 0.65:
				sev = SeverityMedium
			case r < 0.80:
				sev = SeverityLow
			default:
				return nil
			}

			return &Hit{Severity: sev, MetricValue: r}
		},
	},
	{
		// HOT-chain ruptures: an UPDATE could not stay on the same page so a
		// new tuple was put elsewhere. Only meaningful on PG 16+; the
		// 170000/ template returns 0 on older versions and the rule stays silent.
		ID: "high_newpage_update_ratio", Category: "storage", RelatedRoute: "/tables",
		Evaluate: func(m RawMetrics) *Hit {
			r := m.NewpageUpdateRatio

			sev := severityForRatio(r, 0.20, 0.10, 0.05)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: r}
		},
	},
	{
		// Tables that significantly diverged from their last ANALYZE — the
		// planner is making decisions on stale statistics.
		ID: "stale_planner_stats", Category: "maintenance", RelatedRoute: "/maintenance",
		Evaluate: func(m RawMetrics) *Hit {
			sev := severityForCount(m.StalePlannerStatsTables, 10, 5, 3)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: float64(m.StalePlannerStatsTables)}
		},
	},
	{
		// wal_level=minimal forbids streaming replication. If replicas are
		// actually connected, the configuration is internally inconsistent.
		ID: "wal_level_minimal_with_replicas", Category: "wal_checkpoint", RelatedRoute: "/replication",
		Evaluate: func(m RawMetrics) *Hit {
			if m.WalLevel != "minimal" || m.ReplicaCount == 0 {
				return nil
			}

			return &Hit{
				Severity:    SeverityHigh,
				MetricValue: float64(m.ReplicaCount),
				Context:     map[string]any{"replicas": m.ReplicaCount},
			}
		},
	},
	{
		// wal_level=logical without any active logical slot is wasted overhead.
		ID: "wal_level_logical_without_publications", Category: "wal_checkpoint", RelatedRoute: "/replication",
		Evaluate: func(m RawMetrics) *Hit {
			if m.WalLevel != "logical" || m.LogicalSlotsActive > 0 {
				return nil
			}

			return &Hit{Severity: SeverityLow, MetricValue: 0}
		},
	},
}

// severityForRatio returns HIGH/MEDIUM/LOW when value meets/exceeds the
// corresponding threshold. The threshold order is high → medium → low.
func severityForRatio(v, high, med, low float64) Severity {
	switch {
	case v >= high:
		return SeverityHigh
	case v >= med:
		return SeverityMedium
	case v >= low:
		return SeverityLow
	default:
		return ""
	}
}

func severityForCount(v, high, med, low int) Severity {
	switch {
	case v >= high:
		return SeverityHigh
	case v >= med:
		return SeverityMedium
	case v >= low:
		return SeverityLow
	default:
		return ""
	}
}
