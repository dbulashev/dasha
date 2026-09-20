package explain

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/dbulashev/dasha/internal/health"
)

// Requirement is what a rule needs before it can say anything. It is checked
// against what the plan proved it carries, never against how the plan was made.
type Requirement uint8

const (
	ReqActual Requirement = 1 << iota
	ReqTiming
	ReqBuffers
	ReqWorkMem
)

// Missing codes. They travel to the API and the locale files, so they are part
// of the contract.
const (
	MissingActual  = "actual"
	MissingTiming  = "timing"
	MissingBuffers = "buffers"
	MissingWorkMem = "work_mem"
)

// Context is what the caller can add to a plan. Both fields are optional: the
// log track has no connection at parse time and fills in neither.
type Context struct {
	WorkMemKB *int64
	// TableRows maps a relation to its reltuples. Keys are schema-qualified; a
	// bare name is an alias the caller adds only for a relation whose name is
	// unique across schemas, since a plan without VERBOSE carries no schema.
	TableRows map[string]int64
}

func (c Context) tableRows(n *Node) (int64, bool) {
	if len(c.TableRows) == 0 || n.Relation == "" {
		return 0, false
	}

	if n.Schema != "" {
		if rows, ok := c.TableRows[n.Schema+"."+n.Relation]; ok {
			return rows, true
		}
	}

	rows, ok := c.TableRows[n.Relation]

	return rows, ok
}

// Rule is one check over a parsed plan. It carries no prose: a Finding names a
// code and the numbers its phrasing quotes, and the wording lives in the
// frontend locale files and in the MCP knowledge base.
type Rule struct {
	Code     string
	Severity health.Severity
	Requires Requirement
	Eval     func(*Plan, Context) []Finding
}

// Finding is one triggered rule, pinned to the node that triggered it.
type Finding struct {
	Code     string
	Severity health.Severity
	Path     []int // child indexes from the root, for highlighting in the tree
	NodeType string
	Relation string
	Params   map[string]any
}

// Dormant is a rule that could not run, and what the plan would have needed.
type Dormant struct {
	Code    string
	Missing []string
}

// Evaluate runs the catalog. Rules that could not run are part of the answer,
// not silence: a user with log_analyze = off has to see that half the catalog
// never ran, and why.
func Evaluate(p *Plan, ctx Context) (findings []Finding, dormant []Dormant) {
	// The plan's own setting is the one the statement ran with; what the caller
	// knows is the instance default and stands in only where nothing was printed.
	if kb := planWorkMemKB(p); kb != nil {
		ctx.WorkMemKB = kb
	}

	for _, rule := range registry {
		if missing := missingFor(rule.Requires, p.Caps, ctx); len(missing) > 0 {
			dormant = append(dormant, Dormant{Code: rule.Code, Missing: missing})

			continue
		}

		findings = append(findings, rule.Eval(p, ctx)...)
	}

	slices.SortStableFunc(findings, func(a, b Finding) int {
		if c := cmp.Compare(severityRank(a.Severity), severityRank(b.Severity)); c != 0 {
			return c
		}

		return cmp.Compare(a.Code, b.Code)
	})

	return findings, dormant
}

// planWorkMemKB reads the setting the plan printed itself. auto_explain prints
// it only where it differs from the built-in default.
func planWorkMemKB(p *Plan) *int64 {
	kb, ok := ParseMemKB(p.Settings["work_mem"])
	if !ok {
		return nil
	}

	return &kb
}

// ParseMemKB reads a memory setting as PostgreSQL shows it. A bare number is in
// the setting's own unit, kB for work_mem.
func ParseMemKB(v string) (int64, bool) {
	v = strings.TrimSpace(v)
	mult := int64(1)

	switch {
	case strings.HasSuffix(v, "kB"):
		v = strings.TrimSuffix(v, "kB")
	case strings.HasSuffix(v, "MB"):
		v, mult = strings.TrimSuffix(v, "MB"), 1<<10
	case strings.HasSuffix(v, "GB"):
		v, mult = strings.TrimSuffix(v, "GB"), 1<<20
	case strings.HasSuffix(v, "TB"):
		v, mult = strings.TrimSuffix(v, "TB"), 1<<30
	}

	kb, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || kb < 0 {
		return 0, false
	}

	return kb * mult, true
}

func missingFor(req Requirement, caps Capabilities, ctx Context) []string {
	var missing []string

	if req&ReqActual != 0 && !caps.Actual {
		missing = append(missing, MissingActual)
	}

	if req&ReqTiming != 0 && !caps.Timing {
		missing = append(missing, MissingTiming)
	}

	if req&ReqBuffers != 0 && !caps.Buffers {
		missing = append(missing, MissingBuffers)
	}

	if req&ReqWorkMem != 0 && ctx.WorkMemKB == nil {
		missing = append(missing, MissingWorkMem)
	}

	return missing
}

func severityRank(s health.Severity) int {
	switch s {
	case health.SeverityHigh:
		return 0
	case health.SeverityMedium:
		return 1
	default:
		return 2
	}
}

// Rules returns the catalog in evaluation order.
func Rules() []Rule {
	return slices.Clone(registry)
}

// LookupRule finds a rule by code.
func LookupRule(code string) (Rule, bool) {
	for _, r := range registry {
		if r.Code == code {
			return r, true
		}
	}

	return Rule{}, false
}
