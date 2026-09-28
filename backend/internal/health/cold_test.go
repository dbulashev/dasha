package health

import "testing"

func TestRelfrozenxidAgeOutlier_ColdTier(t *testing.T) {
	r := findRule(t, "relfrozenxid_age_outlier")

	cases := []struct {
		name string
		m    RawMetrics
		sev  Severity
		cold bool
	}{
		{"cold below 0.9", RawMetrics{ColdFreezeRatio: 0.85, ColdMaxRelfrozenxidAge: 170_000_000}, "", false},                                                   //nolint:exhaustruct
		{"cold at 0.9", RawMetrics{ColdFreezeRatio: 0.9, ColdMaxRelfrozenxidAge: 180_000_000}, SeverityLow, true},                                               //nolint:exhaustruct
		{"cold over threshold", RawMetrics{ColdFreezeRatio: 1.05, ColdMaxRelfrozenxidAge: 210_000_000}, SeverityMedium, true},                                   //nolint:exhaustruct
		{"cold at failsafe", RawMetrics{ColdFreezeRatio: 8, ColdMaxRelfrozenxidAge: xidFailsafeAge}, SeverityHigh, true},                                        //nolint:exhaustruct
		{"active only", RawMetrics{MaxRelfrozenxidAge: xidFreezeMaxAge}, SeverityMedium, false},                                                                 //nolint:exhaustruct
		{"active wins a tie", RawMetrics{MaxRelfrozenxidAge: xidFreezeTableAge, ColdFreezeRatio: 0.95}, SeverityLow, false},                                     //nolint:exhaustruct
		{"worse cold wins", RawMetrics{MaxRelfrozenxidAge: xidFreezeTableAge, ColdFreezeRatio: 1.2, ColdMaxRelfrozenxidAge: 240_000_000}, SeverityMedium, true}, //nolint:exhaustruct
	}

	for _, tc := range cases {
		hit := r.Evaluate(tc.m)

		if tc.sev == "" {
			if hit != nil {
				t.Errorf("%s: want no hit, got %+v", tc.name, hit)
			}

			continue
		}

		if hit == nil || hit.Severity != tc.sev {
			t.Errorf("%s: want %s, got %+v", tc.name, tc.sev, hit)

			continue
		}

		if got := hit.Context["cold"] == true; got != tc.cold {
			t.Errorf("%s: context cold = %v, want %v", tc.name, got, tc.cold)
		}

		if tc.cold && hit.MetricValue != float64(tc.m.ColdMaxRelfrozenxidAge) {
			t.Errorf("%s: metric = %v, want cold age", tc.name, hit.MetricValue)
		}
	}
}

func coldFacts(n, more int) *ColdFacts {
	tables := make([]ColdMaintenanceTable, n)
	for i := range tables {
		tables[i] = ColdMaintenanceTable{Schema: "public", Table: "t" + string(rune('a'+i)), DeadRatio: 14.24} //nolint:exhaustruct
	}

	return &ColdFacts{WindowDays: 7, Tables: tables, More: more}
}

func TestColdTablesMaintenance(t *testing.T) {
	r := findRule(t, "cold_tables_maintenance")

	if !r.Advisory {
		t.Fatal("cold_tables_maintenance must be advisory")
	}

	if hit := r.Evaluate(RawMetrics{}); hit != nil { //nolint:exhaustruct
		t.Errorf("nil Cold → no hit, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{Cold: coldFacts(0, 0)}); hit != nil { //nolint:exhaustruct
		t.Errorf("empty Cold → no hit, got %+v", hit)
	}

	hit := r.Evaluate(RawMetrics{Cold: coldFacts(5, 12)}) //nolint:exhaustruct
	if hit == nil || hit.Severity != SeverityLow || hit.MetricValue != 17 {
		t.Fatalf("want LOW with 17 tables, got %+v", hit)
	}

	if hit.Context["worst"] != `"public"."ta"` || hit.Context["more"] != 12 || hit.Context["window_days"] != 7 {
		t.Errorf("context = %+v", hit.Context)
	}

	objects, _ := hit.Context["objects"].([]map[string]any)
	if len(objects) != 5 || objects[0]["dead_ratio"] != 14.2 {
		t.Errorf("objects = %+v", objects)
	}

	for _, rec := range Evaluate(RawMetrics{InRecovery: true, Cold: coldFacts(1, 0)}, false) { //nolint:exhaustruct
		if rec.RuleID == "cold_tables_maintenance" {
			t.Error("cold_tables_maintenance must be hidden on a standby")
		}
	}
}
