package health

import "testing"

func TestRegistry_AllRulesEvaluable(t *testing.T) {
	// All rules must produce nil or a Hit; no panic / no missing severity.
	m := RawMetrics{}

	for _, r := range Registry {
		hit := r.Evaluate(m)
		if hit != nil && hit.Severity == "" {
			t.Errorf("rule %q returned Hit without Severity", r.ID)
		}
	}
}

func TestRegistry_NoDuplicateIDs(t *testing.T) {
	seen := make(map[string]bool, len(Registry))
	for _, r := range Registry {
		if seen[r.ID] {
			t.Errorf("duplicate rule ID: %s", r.ID)
		}

		seen[r.ID] = true
	}
}

func TestEvaluate_HealthyDatabaseTriggersNoRules(t *testing.T) {
	m := RawMetrics{
		TotalConnections:    5,
		MaxConnections:      100,
		CacheHitRatio:       99.5,
		ReplicaCount:        1,
		MaxReplayLagSeconds: 0.1,
		MaxXidAge:           50_000_000,
		MaxVacuumAgeHours:   12,
	}

	got := Evaluate(m, false)
	if len(got) != 0 {
		t.Errorf("healthy DB → expect 0 recommendations, got %d: %+v", len(got), got)
	}
}

func TestEvaluate_DatabaseScopeFiltersInstanceOnlyCategories(t *testing.T) {
	// Trigger both an instance-only (connections) and a per-DB (storage) rule.
	m := RawMetrics{
		TotalConnections: 90,
		MaxConnections:   100,
		MaxDeadRatio:     55,
	}

	instance := Evaluate(m, false)
	if len(instance) < 2 {
		t.Fatalf("expected ≥2 recs at instance scope, got %d", len(instance))
	}

	dbScoped := Evaluate(m, true)
	for _, r := range dbScoped {
		if r.Category == "connections" || r.Category == "replication" {
			t.Errorf("db scope should hide %q, but got %+v", r.Category, r)
		}
	}
}

func TestEvaluate_SortedHighFirst(t *testing.T) {
	m := RawMetrics{
		TotalConnections:  98, // HIGH connection_ratio
		MaxConnections:    100,
		IdleInTransaction: 1, // LOW idle_in_tx
		MaxDeadRatio:      25, // MEDIUM dead_ratio
	}

	got := Evaluate(m, false)
	if len(got) < 3 {
		t.Fatalf("expected ≥ 3 recs, got %d", len(got))
	}

	for i := 1; i < len(got); i++ {
		if severityRank(got[i-1].Severity) > severityRank(got[i].Severity) {
			t.Errorf("rec %d (%s) ranked above rec %d (%s)", i-1, got[i-1].Severity, i, got[i].Severity)
		}
	}
}

func TestRule_HighConnectionRatio(t *testing.T) {
	tests := []struct {
		ratio float64
		want  Severity
	}{
		{0.5, ""},
		{0.65, SeverityLow},
		{0.85, SeverityMedium},
		{0.97, SeverityHigh},
	}

	for _, tc := range tests {
		m := RawMetrics{TotalConnections: int(tc.ratio * 100), MaxConnections: 100}
		hit := findRule(t, "high_connection_ratio").Evaluate(m)

		if tc.want == "" {
			if hit != nil {
				t.Errorf("ratio %.2f → want nil, got %v", tc.ratio, hit.Severity)
			}

			continue
		}

		if hit == nil || hit.Severity != tc.want {
			t.Errorf("ratio %.2f → want %s, got %v", tc.ratio, tc.want, hit)
		}
	}
}

func TestRule_HighConnectionRatio_ZeroMaxConnections(t *testing.T) {
	m := RawMetrics{TotalConnections: 50, MaxConnections: 0}
	if hit := findRule(t, "high_connection_ratio").Evaluate(m); hit != nil {
		t.Errorf("zero max_connections must not trigger, got %+v", hit)
	}
}

func TestRule_LowCacheHitRatio(t *testing.T) {
	tests := []struct {
		ratio float64
		want  Severity
	}{
		{99.9, ""},
		{99, ""}, // boundary: below 99 triggers LOW
		{97, SeverityLow},
		{93, SeverityMedium},
		{85, SeverityHigh},
	}

	for _, tc := range tests {
		hit := findRule(t, "low_cache_hit_ratio").Evaluate(RawMetrics{CacheHitRatio: tc.ratio})

		if tc.want == "" {
			if hit != nil {
				t.Errorf("ratio %v → want nil, got %v", tc.ratio, hit.Severity)
			}

			continue
		}

		if hit == nil || hit.Severity != tc.want {
			t.Errorf("ratio %v → want %s, got %v", tc.ratio, tc.want, hit)
		}
	}
}

func TestRule_ReplicationOnlyTriggersWithReplicas(t *testing.T) {
	noRepl := RawMetrics{ReplicaCount: 0, MaxReplayLagSeconds: 60}
	for _, rid := range []string{"replication_lag_time", "replication_lag_bytes"} {
		if hit := findRule(t, rid).Evaluate(noRepl); hit != nil {
			t.Errorf("%s must not trigger without replicas, got %+v", rid, hit)
		}
	}
}

func TestRule_DisconnectedReplicasSeverity(t *testing.T) {
	r := findRule(t, "disconnected_replicas")
	if hit := r.Evaluate(RawMetrics{DisconnectedReplicas: 0}); hit != nil {
		t.Errorf("0 disconnected → nil, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{DisconnectedReplicas: 1}); hit == nil || hit.Severity != SeverityMedium {
		t.Errorf("1 disconnected → MEDIUM, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{DisconnectedReplicas: 3}); hit == nil || hit.Severity != SeverityHigh {
		t.Errorf("3 disconnected → HIGH, got %+v", hit)
	}
}

func TestRule_XidWraparoundRisk(t *testing.T) {
	r := findRule(t, "xid_wraparound_risk")
	cases := []struct {
		age  int64
		want Severity
	}{
		{100_000_000, ""},
		{600_000_000, SeverityLow},
		{1_100_000_000, SeverityMedium},
		{1_800_000_000, SeverityHigh},
	}

	for _, tc := range cases {
		hit := r.Evaluate(RawMetrics{MaxXidAge: tc.age})

		if tc.want == "" {
			if hit != nil {
				t.Errorf("xid_age %d → want nil, got %v", tc.age, hit.Severity)
			}

			continue
		}

		if hit == nil || hit.Severity != tc.want {
			t.Errorf("xid_age %d → want %s, got %v", tc.age, tc.want, hit)
		}
	}
}

func findRule(t *testing.T, id string) Rule {
	t.Helper()

	for _, r := range Registry {
		if r.ID == id {
			return r
		}
	}

	t.Fatalf("rule %q not found in Registry", id)

	return Rule{}
}
