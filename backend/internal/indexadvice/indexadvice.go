// Package indexadvice turns raw per-host index scan samples into a verdict on
// whether an index is safe to drop.
//
// Two facts make the naive "idx_scan == 0 means unused" test dangerous, and this
// package exists to encode both:
//
//   - idx_scan is per-instance and is NOT replicated. An index idle on the primary
//     may be serving the entire read workload on a replica, so a verdict is only
//     valid when every host of the cluster has been consulted — and a host we could
//     not reach must block a "drop it", not be silently skipped.
//   - idx_scan is a counter since the last statistics reset. Zero scans five minutes
//     after pg_stat_reset() proves nothing, and five scans over two years is
//     effectively unused. The meaningful unit is scans per day over a known window.
package indexadvice

import (
	"fmt"
	"strings"

	"github.com/dbulashev/dasha/internal/dto"
)

// Verdict is the recommendation for one index across the whole cluster.
type Verdict string

const (
	// VerdictDropCandidate: no host scanned it over an adequate window.
	VerdictDropCandidate Verdict = "drop_candidate"
	// VerdictUsed: at least one host scans it regularly.
	VerdictUsed Verdict = "used"
	// VerdictStaleEvidence: scanned, but so rarely that the scans may be historical —
	// reset the statistics and observe a full business cycle before deciding.
	VerdictStaleEvidence Verdict = "stale_evidence"
	// VerdictInsufficientData: the statistics window is too short to judge.
	VerdictInsufficientData Verdict = "insufficient_data"
	// VerdictUnknown: a host could not be reached, so the cluster-wide picture is
	// incomplete and no drop can be justified.
	VerdictUnknown Verdict = "unknown"
)

// Default thresholds. MinWindowDays is deliberately short: it is a floor on
// "enough to say anything at all", not a claim that a week proves an index dead —
// the reason text always reports the actual window so a caller can weigh monthly
// reporting jobs, which are the classic reason a seemingly dead index is alive.
const (
	DefaultMinWindowDays   = 7.0
	DefaultUsedScansPerDay = 0.1
)

// Thresholds tunes the verdict. Zero fields fall back to the defaults.
type Thresholds struct {
	MinWindowDays   float64
	UsedScansPerDay float64
}

func (t Thresholds) withDefaults() Thresholds {
	if t.MinWindowDays <= 0 {
		t.MinWindowDays = DefaultMinWindowDays
	}

	if t.UsedScansPerDay <= 0 {
		t.UsedScansPerDay = DefaultUsedScansPerDay
	}

	return t
}

// ReasonCode is the machine-readable form of a verdict's explanation. A UI renders its
// own localized sentence from the code plus ReasonParams; the English prose stays in
// IndexReport.ReasonText for API and MCP clients.
type ReasonCode string

const (
	// ReasonUnreachableHosts: a host did not answer, so the cluster-wide picture is incomplete.
	ReasonUnreachableHosts ReasonCode = "unreachable_hosts"
	// ReasonNoEvidence: no host reported the index at all.
	ReasonNoEvidence ReasonCode = "no_evidence"
	// ReasonUsedOnReplicaOnly: idle on the primary, scanned on a standby.
	ReasonUsedOnReplicaOnly ReasonCode = "used_on_replica_only"
	// ReasonUsed: scanned often enough on at least one host.
	ReasonUsed ReasonCode = "used"
	// ReasonWindowTooShort: the statistics window is below the minimum.
	ReasonWindowTooShort ReasonCode = "window_too_short"
	// ReasonFewScans: scanned, but too rarely to tell recent use from historical.
	ReasonFewScans ReasonCode = "few_scans"
	// ReasonNeverScanned: zero scans everywhere over an adequate window.
	ReasonNeverScanned ReasonCode = "never_scanned"
)

// NoteCode is a caveat that qualifies a reason without changing it.
type NoteCode string

const (
	// NoteStatsResetNever: the window is a lower bound from postmaster uptime.
	NoteStatsResetNever NoteCode = "stats_reset_never"
	// NotePartitioned: the verdict covers a parent index and all its children.
	NotePartitioned NoteCode = "partitioned"
)

// HostRate names a host and the scan rate that keeps the index alive there.
type HostRate struct {
	Instance    string  `json:"instance"`
	ScansPerDay float64 `json:"scans_per_day"`
}

// ReasonParams carries the numbers a reason quotes. Which fields are set depends on
// the code — a renderer reads only the ones its phrasing for that code needs.
type ReasonParams struct {
	Hosts         []string   `json:"hosts,omitempty"`
	UsedOn        []HostRate `json:"used_on,omitempty"`
	WindowDays    float64    `json:"window_days,omitempty"`
	MinWindowDays float64    `json:"min_window_days,omitempty"`
	TotalScans    int64      `json:"total_scans,omitempty"`
	HostCount     int        `json:"host_count,omitempty"`
	// Partitions is internal to the partitioned note: callers already have the count
	// on IndexReport itself.
	Partitions int `json:"-"`
}

// Reason is the explanation behind a verdict. Text renders it in English from the very
// same Code and Params a UI localizes, so the two can never describe different things.
type Reason struct {
	Code   ReasonCode   `json:"code"`
	Notes  []NoteCode   `json:"notes,omitempty"`
	Params ReasonParams `json:"params"`
}

func (r Reason) Text() string {
	parts := make([]string, 0, 1+len(r.Notes))
	parts = append(parts, r.baseText())

	for _, n := range r.Notes {
		parts = append(parts, r.noteText(n))
	}

	return strings.Join(parts, " ")
}

func (r Reason) baseText() string {
	p := r.Params

	switch r.Code {
	case ReasonUnreachableHosts:
		return fmt.Sprintf(
			"cannot judge: %s unreachable. idx_scan is per-instance and is not replicated, "+
				"so an index idle on the hosts we did read may be serving reads on the one we did not.",
			strings.Join(p.Hosts, ", "))

	case ReasonNoEvidence:
		return "no host reported this index."

	case ReasonUsedOnReplicaOnly:
		return fmt.Sprintf(
			"keep: idle on the primary but used on %s. Replica scan counters are not "+
				"replicated back — dropping it would break replica reads.",
			hostRates(p.UsedOn))

	case ReasonUsed:
		return "keep: used on " + hostRates(p.UsedOn) + "."

	case ReasonWindowTooShort:
		return fmt.Sprintf(
			"the shortest statistics window is %.1f day(s), below the %.0f-day minimum — "+
				"wait until it covers a full business cycle (a monthly report can be the only user of an index).",
			p.WindowDays, p.MinWindowDays)

	case ReasonFewScans:
		return fmt.Sprintf(
			"%d scan(s) over %.0f day(s) — too few to call it used, but they may be recent: "+
				"the counter cannot say WHEN. Run pg_stat_reset() and re-check after a full business cycle.",
			p.TotalScans, p.WindowDays)

	case ReasonNeverScanned:
		return fmt.Sprintf("zero scans on all %d host(s) over %.0f day(s).", p.HostCount, p.WindowDays)

	default:
		return ""
	}
}

func (r Reason) noteText(n NoteCode) string {
	switch n {
	case NoteStatsResetNever:
		return "Note: pg_stat_reset() was never called, so this window is only a lower bound taken " +
			"from postmaster uptime — the counters may span much longer (they survive a clean restart), or the " +
			"statistics may have been lost on a crash. Call pg_stat_reset() to establish a window you can trust."

	case NotePartitioned:
		return fmt.Sprintf(
			"This is a partitioned index: the scans of its %d per-partition child indexes are summed here, "+
				"and a DROP must target this parent — PostgreSQL refuses to drop a child and points here instead, "+
				"which removes the index from EVERY partition. A cold partition with zero scans is partition "+
				"pruning working, not a dead index.",
			r.Params.Partitions)

	default:
		return ""
	}
}

func hostRates(hs []HostRate) string {
	out := make([]string, 0, len(hs))
	for _, h := range hs {
		out = append(out, fmt.Sprintf("%s (%.1f scans/day)", h.Instance, h.ScansPerDay))
	}

	return strings.Join(out, ", ")
}

// HostUsage is one host's evidence for one index.
type HostUsage struct {
	Instance    string  `json:"instance"`
	InRecovery  bool    `json:"in_recovery"`
	IndexScans  int64   `json:"index_scans"`
	WindowDays  float64 `json:"window_days"`
	ScansPerDay float64 `json:"scans_per_day"`
	// StatsResetKnown is false when the database's statistics were never reset; the
	// window then falls back to postmaster uptime and understates the real one.
	StatsResetKnown bool `json:"stats_reset_known"`
}

// IndexReport is the cluster-wide verdict for one DROPpable index. For a partitioned
// index that is the top-level parent, with the per-partition children summed into it —
// see Report.
type IndexReport struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	Index  string `json:"index"`
	// Partitioned marks a partitioned index: Index names the parent (the only thing
	// that can be dropped) and Partitions counts the children summed into it.
	Partitioned bool    `json:"partitioned"`
	Partitions  int     `json:"partitions,omitempty"`
	SizeBytes   int64   `json:"size_bytes"`
	Verdict     Verdict `json:"verdict"`
	// Reason is the localizable form; ReasonText is the same explanation in English.
	Reason      Reason      `json:"reason"`
	ReasonText  string      `json:"reason_text"`
	PerInstance []HostUsage `json:"per_instance"`
}

// minWindowFloor guards the per-day division: a window shorter than an hour would
// blow the rate up to a meaningless number.
const minWindowFloor = 1.0 / 24.0

func scansPerDay(scans int64, windowDays float64) float64 {
	return float64(scans) / max(windowDays, minWindowFloor)
}

// hostAgg accumulates one host's counters for one droppable index. For a partitioned
// index that means the sum over all its per-partition children on that host.
type hostAgg struct {
	scans      int64
	size       int64
	windowDays float64
	inRecovery bool
	statsKnown bool
}

// rootAgg accumulates one droppable index across every host.
type rootAgg struct {
	schema, table, index string
	partitioned          bool
	children             map[string]struct{}
	hosts                map[string]*hostAgg
	hostOrder            []string
}

// Report groups the samples by the only unit that can actually be DROPped and assigns
// each one a verdict.
//
// For a plain index that unit is the index. For a partitioned index it is the top-level
// parent, and the per-partition children are SUMMED into it — judging children
// individually is wrong twice over: a cold partition legitimately shows zero scans
// (partition pruning working as designed), and PostgreSQL will not drop a child anyway,
// it points at the parent instead — and dropping the parent removes the index from every
// partition, including the hot ones. Summing means one busy partition is enough to keep
// the whole index, which is exactly the safe answer.
//
// Unreachable hosts downgrade EVERY index to VerdictUnknown: without them the
// cluster-wide picture is incomplete, and the whole point of the cluster-wide view is
// that a single host cannot prove an index unused.
func Report(scans dto.IndexClusterScans, th Thresholds) []IndexReport {
	th = th.withDefaults()

	type key struct{ Schema, Index string }

	order := make([]key, 0, len(scans.Samples))
	roots := make(map[key]*rootAgg, len(scans.Samples))

	for _, hs := range scans.Samples {
		s := hs.Sample
		k := key{s.RootSchema, s.RootIndex}

		r, ok := roots[k]
		if !ok {
			r = &rootAgg{
				schema:      s.RootSchema,
				table:       s.RootTable,
				index:       s.RootIndex,
				partitioned: s.IsPartitioned,
				children:    make(map[string]struct{}),
				hosts:       make(map[string]*hostAgg),
				hostOrder:   nil,
			}
			roots[k] = r
			order = append(order, k)
		}

		r.children[s.Schema+"."+s.Index] = struct{}{}

		h, ok := r.hosts[hs.Instance]
		if !ok {
			// The window and recovery state belong to the host, not the index: every
			// index in a database shares one stats_reset.
			h = &hostAgg{ //nolint:exhaustruct
				windowDays: s.WindowDays,
				inRecovery: s.InRecovery,
				statsKnown: s.StatsReset != nil,
			}
			r.hosts[hs.Instance] = h
			r.hostOrder = append(r.hostOrder, hs.Instance)
		}

		h.scans += s.IndexScans
		h.size += s.SizeBytes
	}

	out := make([]IndexReport, 0, len(order))

	for _, k := range order {
		r := roots[k]

		hosts := make([]HostUsage, 0, len(r.hostOrder))

		var size int64

		for _, inst := range r.hostOrder {
			h := r.hosts[inst]

			// Physical size differs per host (bloat); report the largest — that is what
			// a DROP would actually reclaim.
			size = max(size, h.size)

			hosts = append(hosts, HostUsage{
				Instance:        inst,
				InRecovery:      h.inRecovery,
				IndexScans:      h.scans,
				WindowDays:      h.windowDays,
				ScansPerDay:     scansPerDay(h.scans, h.windowDays),
				StatsResetKnown: h.statsKnown,
			})
		}

		verdict, reason := decide(hosts, scans.Unreachable, th)

		if r.partitioned {
			reason.Params.Partitions = len(r.children)
			reason.Notes = append(reason.Notes, NotePartitioned)
		}

		rep := IndexReport{ //nolint:exhaustruct
			Schema:      r.schema,
			Table:       r.table,
			Index:       r.index,
			Partitioned: r.partitioned,
			SizeBytes:   size,
			Verdict:     verdict,
			Reason:      reason,
			ReasonText:  reason.Text(),
			PerInstance: hosts,
		}

		if r.partitioned {
			rep.Partitions = len(r.children)
		}

		out = append(out, rep)
	}

	return out
}

// decide is the rule itself, kept separate from the grouping so it can be tested
// against a handful of hand-written host samples.
func decide(hosts []HostUsage, unreachable []string, th Thresholds) (Verdict, Reason) {
	if len(unreachable) > 0 {
		return VerdictUnknown, reasonOf(ReasonUnreachableHosts, ReasonParams{Hosts: unreachable}) //nolint:exhaustruct
	}

	if len(hosts) == 0 {
		return VerdictUnknown, reasonOf(ReasonNoEvidence, ReasonParams{}) //nolint:exhaustruct
	}

	var (
		usedOn      []HostRate
		usedOnPrim  bool
		totalScans  int64
		windowGuess bool // some host's window is only a lower bound (stats_reset is NULL)
		minWindow   = hosts[0].WindowDays
	)

	for _, h := range hosts {
		totalScans += h.IndexScans
		minWindow = min(minWindow, h.WindowDays)

		if !h.StatsResetKnown {
			windowGuess = true
		}

		if h.ScansPerDay > th.UsedScansPerDay {
			usedOn = append(usedOn, HostRate{Instance: h.Instance, ScansPerDay: h.ScansPerDay})

			if !h.InRecovery {
				usedOnPrim = true
			}
		}
	}

	// pg_stat_database.stats_reset is NULL until somebody calls pg_stat_reset(), which
	// most databases never do — so this is the COMMON case, not an edge one. The window
	// then falls back to postmaster uptime, which is only a LOWER bound: the counters
	// survive a clean restart, so the real window may be far longer. Say so, instead of
	// quoting the uptime as if it were the truth. Only the verdicts that lean on the
	// window's length carry the note; "used" needs no window to be believed.
	withWindowNote := func(r Reason) Reason {
		if windowGuess {
			r.Notes = append(r.Notes, NoteStatsResetNever)
		}

		return r
	}

	switch {
	case len(usedOn) > 0 && !usedOnPrim:
		return VerdictUsed, reasonOf(ReasonUsedOnReplicaOnly, ReasonParams{UsedOn: usedOn}) //nolint:exhaustruct

	case len(usedOn) > 0:
		return VerdictUsed, reasonOf(ReasonUsed, ReasonParams{UsedOn: usedOn}) //nolint:exhaustruct

	case minWindow < th.MinWindowDays:
		return VerdictInsufficientData, withWindowNote(reasonOf(ReasonWindowTooShort, ReasonParams{ //nolint:exhaustruct
			WindowDays: minWindow, MinWindowDays: th.MinWindowDays,
		}))

	case totalScans > 0:
		return VerdictStaleEvidence, reasonOf(ReasonFewScans, ReasonParams{ //nolint:exhaustruct
			TotalScans: totalScans, WindowDays: minWindow,
		})

	default:
		return VerdictDropCandidate, withWindowNote(reasonOf(ReasonNeverScanned, ReasonParams{ //nolint:exhaustruct
			HostCount: len(hosts), WindowDays: minWindow,
		}))
	}
}

func reasonOf(code ReasonCode, p ReasonParams) Reason {
	return Reason{Code: code, Notes: nil, Params: p}
}
