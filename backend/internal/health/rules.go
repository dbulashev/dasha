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

// instanceCategoryFlags caches which categories make sense for the per-database
// drilldown. Used to filter rules when database != "".
var instanceOnlyCategories = map[string]bool{
	"connections": true,
	"replication": true,
}

// Evaluate runs all rules against the given metrics and returns triggered
// recommendations, sorted by severity (HIGH first) and then by rule ID for
// stable output.
//
// If databaseScoped is true, instance-only categories (connections, replication)
// are skipped — they have no meaning at the per-database level.
func Evaluate(m RawMetrics, databaseScoped bool) []Recommendation {
	out := make([]Recommendation, 0, len(Registry))

	for _, r := range Registry {
		if databaseScoped && instanceOnlyCategories[r.Category] {
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
			sev := severityForCount(m.IdleInTransaction, 5, 2, 1)
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

			var sev Severity

			switch {
			case r < 90:
				sev = SeverityHigh
			case r < 95:
				sev = SeverityMedium
			case r < 99:
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
			sev := severityForRatio(m.MaxDeadRatio, 50, 20, 10)
			if sev == "" {
				return nil
			}

			return &Hit{Severity: sev, MetricValue: m.MaxDeadRatio}
		},
	},
	{
		ID: "high_avg_dead_ratio", Category: "storage", RelatedRoute: "/tables",
		Evaluate: func(m RawMetrics) *Hit {
			sev := severityForRatio(m.AvgDeadRatio, 25, 10, 5)
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
		ID: "xid_wraparound_risk", Category: "maintenance", RelatedRoute: "/maintenance",
		Evaluate: func(m RawMetrics) *Hit {
			age := float64(m.MaxXidAge)

			sev := severityForRatio(age, 1_500_000_000, 1_000_000_000, 500_000_000)
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

			sev := severityForRatio(days, 14, 7, 2)
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
