package indexadvice

import (
	"strings"
	"testing"
	"time"

	"github.com/dbulashev/dasha/internal/dto"
)

// Every code the package can produce. A code added to indexadvice.go but not here is
// still caught at runtime by the loud unknownCode text — this list is what catches it
// at build time instead, so keep it complete.
var (
	allReasonCodes = []ReasonCode{
		ReasonUnreachableHosts, ReasonNoEvidence, ReasonUsedOnReplicaOnly, ReasonUsed,
		ReasonWindowTooShort, ReasonFewScans, ReasonNeverScanned,
	}
	allNoteCodes = []NoteCode{NoteStatsResetNever, NotePartitioned}
)

// A code with no sentence used to render as "", quietly shortening the explanation.
// It now renders a loud marker, and this test fails before that ever ships.
func TestEveryCodeRendersProse(t *testing.T) {
	t.Parallel()

	for _, c := range allReasonCodes {
		r := Reason{Code: c, Notes: nil, Params: ReasonParams{}} //nolint:exhaustruct

		got := r.baseText()
		if got == "" || strings.Contains(got, "unknown reason code") {
			t.Errorf("reason code %q renders no prose: %q", c, got)
		}
	}

	for _, n := range allNoteCodes {
		r := Reason{Code: "", Notes: nil, Params: ReasonParams{}} //nolint:exhaustruct

		got := r.noteText(n)
		if got == "" || strings.Contains(got, "unknown note code") {
			t.Errorf("note code %q renders no prose: %q", n, got)
		}
	}
}

func sample(
	instance string,
	inRecovery bool,
	scans int64,
	windowDays float64,
	sizeBytes int64,
	statsReset *time.Time,
) dto.IndexHostSample {
	return dto.IndexHostSample{
		Instance: instance,
		Sample: dto.IndexScanSample{
			Schema:        "public",
			Table:         "orders",
			Index:         "idx_orders_tags",
			RootSchema:    "public",
			RootIndex:     "idx_orders_tags", // standalone: it is its own droppable unit
			RootTable:     "orders",
			IsPartitioned: false,
			SizeBytes:     sizeBytes,
			IndexScans:    scans,
			StatsReset:    statsReset,
			WindowDays:    windowDays,
			InRecovery:    inRecovery,
		},
	}
}

// partSample is one PARTITION's child index, all children rolling up to the same
// top-level partitioned index evt_tag_idx.
func partSample(instance, partition string, scans int64, sizeBytes int64) dto.IndexHostSample {
	now := time.Now()

	return dto.IndexHostSample{
		Instance: instance,
		Sample: dto.IndexScanSample{
			Schema:        "public",
			Table:         partition,
			Index:         partition + "_tag_idx",
			RootSchema:    "public",
			RootIndex:     "evt_tag_idx", // the parent — the only droppable unit
			RootTable:     "evt",
			IsPartitioned: true,
			SizeBytes:     sizeBytes,
			IndexScans:    scans,
			StatsReset:    &now,
			WindowDays:    60,
			InRecovery:    false,
		},
	}
}

func clusterScans(samples ...dto.IndexHostSample) dto.IndexClusterScans {
	return dto.IndexClusterScans{Samples: samples, Unreachable: nil}
}

func host(name string, inRecovery bool, scans int64, windowDays float64) HostUsage {
	return HostUsage{
		Instance:        name,
		InRecovery:      inRecovery,
		IndexScans:      scans,
		WindowDays:      windowDays,
		ScansPerDay:     scansPerDay(scans, windowDays),
		StatsResetKnown: true,
	}
}

func TestDecide(t *testing.T) {
	t.Parallel()

	th := Thresholds{}.withDefaults()

	tests := []struct {
		name        string
		hosts       []HostUsage
		unreachable []string
		want        Verdict
		wantCode    ReasonCode
		reasonHas   string
	}{
		{
			name:     "zero scans everywhere over a long window",
			hosts:    []HostUsage{host("primary", false, 0, 60), host("replica", true, 0, 60)},
			want:     VerdictDropCandidate,
			wantCode: ReasonNeverScanned,
		},
		{
			// The case the cluster-wide view exists for: idle on the primary, hot on a
			// replica. A per-instance check against the primary would say "drop it".
			name:      "used only on a replica",
			hosts:     []HostUsage{host("primary", false, 0, 60), host("replica", true, 90000, 60)},
			want:      VerdictUsed,
			wantCode:  ReasonUsedOnReplicaOnly,
			reasonHas: "replica",
		},
		{
			name:     "used on the primary",
			hosts:    []HostUsage{host("primary", false, 90000, 60), host("replica", true, 0, 60)},
			want:     VerdictUsed,
			wantCode: ReasonUsed,
		},
		{
			// Zero scans, but the statistics were reset an hour ago — proves nothing.
			name:     "window too short",
			hosts:    []HostUsage{host("primary", false, 0, 0.04)},
			want:     VerdictInsufficientData,
			wantCode: ReasonWindowTooShort,
		},
		{
			// A short window on ONE host must be enough to withhold the verdict: that
			// host's silence is not evidence.
			name:     "one host has a short window",
			hosts:    []HostUsage{host("primary", false, 0, 400), host("replica", true, 0, 0.5)},
			want:     VerdictInsufficientData,
			wantCode: ReasonWindowTooShort,
		},
		{
			// Scanned 3 times in a year: the rate is negligible, but the counter cannot
			// say whether those scans were last January or this morning.
			name:      "a few scans over a long window",
			hosts:     []HostUsage{host("primary", false, 3, 365)},
			want:      VerdictStaleEvidence,
			wantCode:  ReasonFewScans,
			reasonHas: "pg_stat_reset",
		},
		{
			// An unreachable host must never yield a drop recommendation, even though
			// every host we DID read shows zero scans.
			name:        "a host is unreachable",
			hosts:       []HostUsage{host("primary", false, 0, 400)},
			unreachable: []string{"replica"},
			want:        VerdictUnknown,
			wantCode:    ReasonUnreachableHosts,
			reasonHas:   "replica",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, reason := decide(tt.hosts, tt.unreachable, th)
			if got != tt.want {
				t.Errorf("decide() = %q, want %q (reason: %s)", got, tt.want, reason.Text())
			}

			if tt.wantCode != "" && reason.Code != tt.wantCode {
				t.Errorf("decide() code = %q, want %q", reason.Code, tt.wantCode)
			}

			// The English text is rendered from the code and params a UI localizes, so
			// checking it also checks that those carry what the sentence needs.
			if tt.reasonHas != "" && !strings.Contains(reason.Text(), tt.reasonHas) {
				t.Errorf("reason %q does not mention %q", reason.Text(), tt.reasonHas)
			}
		})
	}
}

// A drop_candidate must never be produced from an incomplete picture — this is the
// property that makes the report safe to act on.
func TestDecide_UnreachableHostNeverDrops(t *testing.T) {
	t.Parallel()

	th := Thresholds{}.withDefaults()

	got, _ := decide([]HostUsage{host("primary", false, 0, 999)}, []string{"replica-1"}, th)
	if got == VerdictDropCandidate {
		t.Fatalf("decide() returned %q with an unreachable host — a missing host must block a drop", got)
	}
}

// The partitioning trap. pg_stat_user_indexes lists only the per-partition child
// indexes, never the parent, so a cold partition shows zero scans — that is partition
// pruning working, not a dead index. Judged individually, evt_jan_tag_idx would be a
// drop_candidate; and acting on that is worse than useless, because PostgreSQL refuses
// to drop a child and its HINT points at the parent, which would strip the index off
// EVERY partition — including the hot one. Summing the children to the parent makes one
// busy partition enough to keep the whole index.
func TestReport_PartitionedIndexRollsUpToParent(t *testing.T) {
	t.Parallel()

	got := Report(clusterScans(
		partSample("primary", "evt_jan", 0, 100),    // cold partition: nobody queries January
		partSample("primary", "evt_feb", 9000, 100), // hot partition
	), Thresholds{}) //nolint:exhaustruct

	if len(got) != 1 {
		t.Fatalf("Report() returned %d entries, want 1 — both children must roll up to the parent index", len(got))
	}

	r := got[0]
	if r.Index != "evt_tag_idx" || r.Table != "evt" {
		t.Errorf("reported %s on %s, want the parent evt_tag_idx on evt — a child cannot be dropped", r.Index, r.Table)
	}

	if !r.Partitioned || r.Partitions != 2 {
		t.Errorf("Partitioned=%v Partitions=%d, want true/2", r.Partitioned, r.Partitions)
	}

	if r.Verdict != VerdictUsed {
		t.Fatalf("Verdict = %q, want %q: the hot partition alone must keep the whole index alive", r.Verdict, VerdictUsed)
	}

	if r.SizeBytes != 200 {
		t.Errorf("SizeBytes = %d, want 200 (both children — a DROP of the parent reclaims all of them)", r.SizeBytes)
	}
}

// With every partition cold, the parent really is unused — and then the drop target is
// the parent, not a child.
func TestReport_PartitionedIndexUnusedEverywhere(t *testing.T) {
	t.Parallel()

	got := Report(clusterScans(
		partSample("primary", "evt_jan", 0, 100),
		partSample("primary", "evt_feb", 0, 100),
	), Thresholds{}) //nolint:exhaustruct

	if len(got) != 1 || got[0].Verdict != VerdictDropCandidate {
		t.Fatalf("want a single drop_candidate, got %d entries with verdict %q", len(got), got[0].Verdict)
	}

	if got[0].Index != "evt_tag_idx" {
		t.Errorf("drop target = %q, want the parent evt_tag_idx", got[0].Index)
	}
}

func TestReport_GroupsHostsAndKeepsLargestSize(t *testing.T) {
	t.Parallel()

	now := time.Now()
	scans := clusterScans(
		sample("primary", false, 0, 60, 100, &now),
		sample("replica", true, 0, 60, 180, &now), // bloated copy on the replica
	)

	got := Report(scans, Thresholds{}) //nolint:exhaustruct
	if len(got) != 1 {
		t.Fatalf("Report() returned %d indexes, want 1 (both hosts describe the same index)", len(got))
	}

	r := got[0]
	if len(r.PerInstance) != 2 {
		t.Errorf("PerInstance = %d hosts, want 2", len(r.PerInstance))
	}

	if r.SizeBytes != 180 {
		t.Errorf("SizeBytes = %d, want 180 (the largest copy — that is what a DROP reclaims)", r.SizeBytes)
	}

	if r.Verdict != VerdictDropCandidate {
		t.Errorf("Verdict = %q, want %q", r.Verdict, VerdictDropCandidate)
	}
}
