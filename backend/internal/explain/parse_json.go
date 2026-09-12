package explain

import (
	"encoding/json"
	"strconv"
	"strings"
)

type jsonPlan struct {
	QueryText     string         `json:"Query Text"`
	QueryID       *int64         `json:"Query Identifier"`
	Plan          *jsonNode      `json:"Plan"`
	PlanningTime  *float64       `json:"Planning Time"`
	ExecutionTime *float64       `json:"Execution Time"`
	Triggers      []jsonTrigger  `json:"Triggers"`
	Settings      map[string]any `json:"Settings"`
	JIT           *jsonJIT       `json:"JIT"`
}

type jsonTrigger struct {
	Name     string  `json:"Trigger Name"`
	Relation string  `json:"Relation"`
	Time     float64 `json:"Time"`
	Calls    float64 `json:"Calls"`
}

type jsonJIT struct {
	Functions int `json:"Functions"`
	Timing    struct {
		Generation   *float64 `json:"Generation"`
		Inlining     *float64 `json:"Inlining"`
		Optimization *float64 `json:"Optimization"`
		Emission     *float64 `json:"Emission"`
		Total        *float64 `json:"Total"`
	} `json:"Timing"`
}

type jsonNode struct {
	NodeType      string `json:"Node Type"`
	Strategy      string `json:"Strategy"`
	PartialMode   string `json:"Partial Mode"`
	Operation     string `json:"Operation"`
	JoinType      string `json:"Join Type"`
	ScanDirection string `json:"Scan Direction"`
	ParentRel     string `json:"Parent Relationship"`
	SubplanName   string `json:"Subplan Name"`
	ParallelAware bool   `json:"Parallel Aware"`

	Relation  string `json:"Relation Name"`
	Schema    string `json:"Schema"`
	Alias     string `json:"Alias"`
	IndexName string `json:"Index Name"`
	CTEName   string `json:"CTE Name"`
	FuncName  string `json:"Function Name"`

	StartupCost float64 `json:"Startup Cost"`
	TotalCost   float64 `json:"Total Cost"`
	PlanRows    float64 `json:"Plan Rows"`
	PlanWidth   int     `json:"Plan Width"`

	ActualStartupTime *float64 `json:"Actual Startup Time"`
	ActualTotalTime   *float64 `json:"Actual Total Time"`
	ActualRows        *float64 `json:"Actual Rows"`
	ActualLoops       *float64 `json:"Actual Loops"`

	Filter      string   `json:"Filter"`
	IndexCond   string   `json:"Index Cond"`
	RecheckCond string   `json:"Recheck Cond"`
	HashCond    string   `json:"Hash Cond"`
	MergeCond   string   `json:"Merge Cond"`
	JoinFilter  string   `json:"Join Filter"`
	TIDCond     string   `json:"TID Cond"`
	SortKey     []string `json:"Sort Key"`

	RowsRemovedByFilter     *float64 `json:"Rows Removed by Filter"`
	RowsRemovedByJoinFilter *float64 `json:"Rows Removed by Join Filter"`
	HeapFetches             *float64 `json:"Heap Fetches"`
	SortMethod              string   `json:"Sort Method"`
	SortSpaceUsed           *float64 `json:"Sort Space Used"`
	SortSpaceType           string   `json:"Sort Space Type"`
	WorkersPlanned          *int     `json:"Workers Planned"`
	WorkersLaunched         *int     `json:"Workers Launched"`
	ExactHeapBlocks         *float64 `json:"Exact Heap Blocks"`
	LossyHeapBlocks         *float64 `json:"Lossy Heap Blocks"`

	SharedHit     *float64 `json:"Shared Hit Blocks"`
	SharedRead    *float64 `json:"Shared Read Blocks"`
	SharedDirtied *float64 `json:"Shared Dirtied Blocks"`
	SharedWritten *float64 `json:"Shared Written Blocks"`
	LocalHit      *float64 `json:"Local Hit Blocks"`
	LocalRead     *float64 `json:"Local Read Blocks"`
	LocalDirtied  *float64 `json:"Local Dirtied Blocks"`
	LocalWritten  *float64 `json:"Local Written Blocks"`
	TempRead      *float64 `json:"Temp Read Blocks"`
	TempWritten   *float64 `json:"Temp Written Blocks"`

	Plans []jsonNode `json:"Plans"`
}

// ParseJSON reads both shapes PostgreSQL produces: the bare object auto_explain
// logs and the single-element array EXPLAIN returns.
func ParseJSON(body string, src Source) (Plan, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return Plan{}, &Error{Code: CodeEmptyPlan}
	}

	var raw jsonPlan

	if trimmed[0] == '[' {
		var list []jsonPlan
		if err := json.Unmarshal([]byte(trimmed), &list); err != nil {
			return Plan{}, parseErrorf("json: %v", err)
		}

		if len(list) == 0 {
			return Plan{}, &Error{Code: CodeEmptyPlan}
		}

		raw = list[0]
	} else if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return Plan{}, parseErrorf("json: %v", err)
	}

	if raw.Plan == nil {
		return Plan{}, parseErrorf("no Plan element")
	}

	p := Plan{
		Source:        src,
		Format:        FormatJSON,
		QueryText:     raw.QueryText,
		PlanningTime:  raw.PlanningTime,
		ExecutionTime: raw.ExecutionTime,
	}

	p.Root = convertJSONNode(raw.Plan, &p.Caps)

	if raw.QueryID != nil {
		p.QueryID, p.HasQueryID = *raw.QueryID, true
	}

	for _, t := range raw.Triggers {
		p.Triggers = append(p.Triggers, Trigger{Name: t.Name, Relation: t.Relation, Time: t.Time, Calls: t.Calls})
	}

	if len(raw.Settings) > 0 {
		p.Settings = make(map[string]string, len(raw.Settings))
		for k, v := range raw.Settings {
			p.Settings[k] = settingString(v)
		}
	}

	if raw.JIT != nil {
		p.JIT = &JIT{
			Functions:    raw.JIT.Functions,
			Generation:   raw.JIT.Timing.Generation,
			Inlining:     raw.JIT.Timing.Inlining,
			Optimization: raw.JIT.Timing.Optimization,
			Emission:     raw.JIT.Timing.Emission,
			Total:        raw.JIT.Timing.Total,
		}
	}

	p.Caps.Settings = len(p.Settings) > 0
	if p.ExecutionTime != nil {
		p.Caps.Timing = true
	}

	return p, nil
}

func settingString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "on"
		}

		return "off"
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}

		return string(b)
	}
}

func convertJSONNode(j *jsonNode, caps *Capabilities) Node {
	n := Node{
		Type:          j.NodeType,
		Relation:      j.Relation,
		Schema:        j.Schema,
		Alias:         j.Alias,
		IndexName:     j.IndexName,
		ParentRel:     j.ParentRel,
		SubplanName:   j.SubplanName,
		Parallel:      j.ParallelAware,
		PartialMode:   normalizePartialMode(j.PartialMode),
		Strategy:      j.Strategy,
		JoinType:      j.JoinType,
		ScanDirection: j.ScanDirection,
		Operation:     j.Operation,

		StartupCost: j.StartupCost,
		TotalCost:   j.TotalCost,
		PlanRows:    j.PlanRows,
		PlanWidth:   j.PlanWidth,

		Filter:              j.Filter,
		IndexCond:           firstNonEmpty(j.IndexCond, j.TIDCond),
		RecheckCond:         j.RecheckCond,
		JoinCond:            firstNonEmpty(j.HashCond, j.MergeCond, j.JoinFilter),
		SortKey:             j.SortKey,
		RowsRemovedByFilter: firstNonNil(j.RowsRemovedByFilter, j.RowsRemovedByJoinFilter),
		HeapFetches:         j.HeapFetches,
		SortMethod:          j.SortMethod,
		SortSpaceKB:         j.SortSpaceUsed,
		SortSpaceType:       j.SortSpaceType,
		WorkersPlanned:      j.WorkersPlanned,
		WorkersLaunched:     j.WorkersLaunched,
		HeapBlocksExact:     j.ExactHeapBlocks,
		HeapBlocksLossy:     j.LossyHeapBlocks,
	}

	if n.Relation == "" {
		n.Relation = firstNonEmpty(j.CTEName, j.FuncName)
	}

	if n.Alias == n.Relation {
		n.Alias = ""
	}

	if j.ActualRows != nil && j.ActualLoops != nil {
		n.Actual = &Actual{Rows: *j.ActualRows, Loops: *j.ActualLoops}
		caps.Actual = true

		if j.ActualStartupTime != nil {
			n.Actual.StartupTime = *j.ActualStartupTime
		}

		if j.ActualTotalTime != nil {
			n.Actual.TotalTime = *j.ActualTotalTime
			caps.Timing = true
		}
	}

	n.Buffers = jsonBuffers(j)
	if n.Buffers != nil {
		caps.Buffers = true
	}

	for i := range j.Plans {
		n.Children = append(n.Children, convertJSONNode(&j.Plans[i], caps))
	}

	return n
}

func jsonBuffers(j *jsonNode) *Buffers {
	fields := []*float64{
		j.SharedHit, j.SharedRead, j.SharedDirtied, j.SharedWritten,
		j.LocalHit, j.LocalRead, j.LocalDirtied, j.LocalWritten,
		j.TempRead, j.TempWritten,
	}

	present := false
	for _, f := range fields {
		if f != nil {
			present = true

			break
		}
	}

	if !present {
		return nil
	}

	return &Buffers{
		SharedHit:     deref(j.SharedHit),
		SharedRead:    deref(j.SharedRead),
		SharedDirtied: deref(j.SharedDirtied),
		SharedWritten: deref(j.SharedWritten),
		LocalHit:      deref(j.LocalHit),
		LocalRead:     deref(j.LocalRead),
		LocalDirtied:  deref(j.LocalDirtied),
		LocalWritten:  deref(j.LocalWritten),
		TempRead:      deref(j.TempRead),
		TempWritten:   deref(j.TempWritten),
	}
}

// normalizePartialMode drops the JSON-only "Simple", which the text format does
// not print at all.
func normalizePartialMode(mode string) string {
	if mode == "Simple" {
		return ""
	}

	return mode
}

func deref(v *float64) float64 {
	if v == nil {
		return 0
	}

	return *v
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}

	return ""
}

func firstNonNil(vs ...*float64) *float64 {
	for _, v := range vs {
		if v != nil {
			return v
		}
	}

	return nil
}
