package schemalint

import "testing"

func TestRegistry_NoDuplicateCodes(t *testing.T) {
	seen := make(map[string]bool, len(registry))

	for _, c := range registry {
		if seen[c.Code] {
			t.Errorf("duplicate check code: %s", c.Code)
		}

		seen[c.Code] = true
	}
}

func TestRegistry_EveryCheckIsComplete(t *testing.T) {
	for _, c := range registry {
		if c.ObjectType == "" || c.RelatedRoute == "" || c.Query == "" {
			t.Errorf("%s: object type, related route and query are part of the contract", c.Code)
		}

		if c.MinVersion < MinSupportedVersion {
			t.Errorf("%s: min version %d is below the supported baseline", c.Code, c.MinVersion)
		}
	}
}

func TestPlan_DeduplicatesSharedQueries(t *testing.T) {
	plans, skips := Plan(Config{}, 160000)

	// On PG16 the only thing a default config leaves out is the opt-in checks.
	defaultOff := 0

	for _, c := range registry {
		if !c.DefaultEnabled {
			defaultOff++
		}
	}

	if len(skips) != defaultOff {
		t.Errorf("expected %d skipped (default-off) checks, got %+v", defaultOff, skips)
	}

	for _, s := range skips {
		if s.Reason != SkipDisabled {
			t.Errorf("%s: reason = %s, want disabled", s.Code, s.Reason)
		}
	}

	seen := make(map[string]bool, len(plans))
	codes := 0

	for _, p := range plans {
		if seen[p.Query.String()] {
			t.Errorf("query %s planned twice", p.Query)
		}

		seen[p.Query.String()] = true
		codes += len(p.Codes)
	}

	if codes != len(registry)-defaultOff {
		t.Errorf("planned %d codes, expected %d", codes, len(registry)-defaultOff)
	}

	// no_primary_key and no_unique_key read the same rows and must share a query.
	for _, p := range plans {
		if len(p.Codes) > 1 {
			return
		}
	}

	t.Error("expected at least one query serving several checks")
}

func TestPlan_SkipsChecksBelowTheirMinVersion(t *testing.T) {
	plans, skips := Plan(Config{}, 130000)

	if len(plans) != 0 {
		t.Errorf("PG13 is below the baseline, nothing should run: %+v", plans)
	}

	if len(skips) != len(registry) {
		t.Fatalf("every check should be skipped as unsupported, got %d of %d", len(skips), len(registry))
	}

	reasons := make(map[string]SkipReason, len(skips))
	for _, s := range skips {
		reasons[s.Code] = s.Reason
	}

	for _, c := range registry {
		// A check switched off in the config is reported as such; the version is
		// only the reason for the ones that would otherwise have run.
		want := SkipUnsupportedVersion
		if !c.DefaultEnabled {
			want = SkipDisabled
		}

		if reasons[c.Code] != want {
			t.Errorf("%s: reason = %s, want %s", c.Code, reasons[c.Code], want)
		}
	}
}

func TestPlan_UnknownVersionRunsEverything(t *testing.T) {
	// Version 0 means "not determined" — better to run and let the query fail
	// into a skip than to silently report nothing.
	plans, _ := Plan(Config{}, 0)
	if len(plans) == 0 {
		t.Error("an unknown server version must not disable every check")
	}
}

func TestConfig_EnabledChecksOptInAndDisabledWins(t *testing.T) {
	chk := Check{Code: "x", DefaultEnabled: false}

	if (Config{}).enabled(chk) {
		t.Error("a default-off check must stay off without config")
	}

	if !(Config{EnabledChecks: []string{"x"}}).enabled(chk) {
		t.Error("enabled_checks must switch a default-off check on")
	}

	both := Config{EnabledChecks: []string{"x"}, DisabledChecks: []string{"x"}}
	if both.enabled(chk) {
		t.Error("disabled_checks must win: silencing a check cannot be undone by another list")
	}
}

func TestRequiredPrivilege_ReportedForSequences(t *testing.T) {
	if RequiredPrivilege(CodeSequenceExhaustion) == "" {
		t.Error("the sequence check reads last_value and must name the grant it needs")
	}

	if RequiredPrivilege("nonexistent") != "" {
		t.Error("an unknown code has no privilege hint")
	}
}
