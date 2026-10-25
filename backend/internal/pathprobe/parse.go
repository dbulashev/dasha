package pathprobe

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

const (
	CodeSyntax       = "trace_syntax"        // not JSON
	CodeType         = "trace_type"          // a field of the wrong JSON type
	CodeNotPathprobe = "trace_not_pathprobe" // JSON, but no events
	CodeInvalid      = "trace_invalid"       // a value pathprobe never writes
)

// Error never carries any part of the input: it may end up in a log.
type Error struct {
	Code   string
	Field  string
	Offset int64
}

func (e *Error) Error() string {
	var b strings.Builder

	b.WriteString("pathprobe trace: ")
	b.WriteString(e.Code)

	if e.Field != "" {
		b.WriteString(" at ")
		b.WriteString(e.Field)
	}

	if e.Offset > 0 {
		b.WriteString(" offset ")
		b.WriteString(strconv.FormatInt(e.Offset, 10))
	}

	return b.String()
}

type rawTrace struct {
	Query    string            `json:"query"`
	Rels     map[string]string `json:"rels"`
	Events   *[]rawEvent       `json:"events"`
	Paths    []rawPath         `json:"paths"`
	Coverage map[string]bool   `json:"coverage"`
	Limits   *rawLimits        `json:"limits"`
}

type rawEvent struct {
	Seq           int      `json:"seq"`
	Src           string   `json:"src"`
	Rel           string   `json:"rel"`
	Kind          string   `json:"kind"`
	PathType      string   `json:"pathtype"`
	PathID        *int     `json:"path_id"`
	CostStartup   *float64 `json:"cost_startup"`
	CostTotal     *float64 `json:"cost_total"`
	Gap           *float64 `json:"gap_cost"`
	Param         bool     `json:"param"`
	Label         *string  `json:"label"`
	KeptReason    *string  `json:"kept_reason"`
	Origin        string   `json:"origin"`
	SkipReason    *string  `json:"skip_reason"`
	DisplacerID   *int     `json:"displacer_path_id"`
	DisplacerSeq  *int     `json:"displacer_seq"`
	OtherType     *string  `json:"other_pathtype"`
	DominatorID   *int     `json:"dominator_path_id"`
	DominatorSeq  *int     `json:"dominator_seq"`
	DominatorType *string  `json:"dominator_pathtype"`
	RemoverID     *int     `json:"remover_path_id"`
	RemoverSeq    *int     `json:"remover_seq"`
	RemoverType   *string  `json:"remover_pathtype"`
	DisabledNodes int      `json:"disabled_nodes"`
}

type rawPath struct {
	ID            int      `json:"path_id"`
	PathType      string   `json:"pathtype"`
	Rel           string   `json:"rel"`
	SubID         int      `json:"subpath_id"`
	OuterID       int      `json:"outer_path_id"`
	InnerID       int      `json:"inner_path_id"`
	ChildIDs      []int    `json:"child_path_ids"`
	Rows          *float64 `json:"rows"`
	RequiredOuter *string  `json:"required_outer"`
	IndexOID      uint32   `json:"index_oid"`
	IndexName     *string  `json:"index_name"`
	IndexHypo     bool     `json:"hypothetical_index"`
}

type rawLimits struct {
	MaxEvents        int  `json:"max_events"`
	EventsTruncated  bool `json:"events_truncated"`
	SummaryTruncated bool `json:"summary_truncated"`
}

var knownSrc = map[string]bool{"pc": true, "p1": true, "p0": true, "j2": true, "wp": true}

func Parse(body []byte) (Trace, error) {
	var raw rawTrace

	if err := json.Unmarshal(body, &raw); err != nil {
		return Trace{}, decodeError(err)
	}

	if raw.Events == nil {
		return Trace{}, &Error{Code: CodeNotPathprobe, Field: "events", Offset: 0}
	}

	tr := Trace{ //nolint:exhaustruct
		Query: raw.Query,
		Rels:  make(map[int]string, len(raw.Rels)),
		Paths: make(map[int]PathNode, len(raw.Paths)),
	}

	for k, alias := range raw.Rels {
		n, err := strconv.Atoi(k)
		if err != nil || n <= 0 {
			return Trace{}, &Error{Code: CodeInvalid, Field: "rels", Offset: 0}
		}

		tr.Rels[n] = alias
	}

	unknown := map[string]bool{}

	tr.Events = make([]Event, 0, len(*raw.Events))

	for i, re := range *raw.Events {
		ev, err := convertEvent(re, unknown)
		if err != nil {
			err.Field = fmt.Sprintf("events[%d].%s", i, err.Field)

			return Trace{}, err
		}

		tr.Events = append(tr.Events, ev)
	}

	for i, rp := range raw.Paths {
		p, err := convertPath(rp)
		if err != nil {
			err.Field = fmt.Sprintf("paths[%d].%s", i, err.Field)

			return Trace{}, err
		}

		if _, dup := tr.Paths[p.ID]; dup {
			return Trace{}, &Error{Code: CodeInvalid, Field: fmt.Sprintf("paths[%d].path_id", i), Offset: 0}
		}

		tr.Paths[p.ID] = p
	}

	markSubqueries(tr.Paths)

	tr.Coverage = convertCoverage(raw.Coverage)

	if raw.Limits != nil {
		tr.Limits = Limits{
			MaxEvents:        raw.Limits.MaxEvents,
			EventsTruncated:  raw.Limits.EventsTruncated,
			SummaryTruncated: raw.Limits.SummaryTruncated,
		}
	}

	for v := range unknown {
		tr.Unknown = append(tr.Unknown, v)
	}

	sortStrings(tr.Unknown)

	return tr, nil
}

func convertEvent(re rawEvent, unknown map[string]bool) (Event, *Error) {
	rel, ok := relKey(re.Rel)
	if !ok {
		return Event{}, &Error{Code: CodeInvalid, Field: "rel", Offset: 0}
	}

	ev := Event{ //nolint:exhaustruct
		Seq:           re.Seq,
		Src:           re.Src,
		Rel:           rel,
		Kind:          Kind(re.Kind),
		PathType:      re.PathType,
		CostStartup:   re.CostStartup,
		CostTotal:     re.CostTotal,
		Gap:           re.Gap,
		Param:         re.Param,
		Label:         re.Label,
		KeptReason:    re.KeptReason,
		Origin:        re.Origin,
		SkipReason:    re.SkipReason,
		DisabledNodes: re.DisabledNodes,
	}

	if re.PathID != nil {
		ev.PathID = *re.PathID
	}

	// the three links are exclusive by kind
	for _, by := range []struct {
		id, seq *int
		typ     *string
	}{
		{re.DisplacerID, re.DisplacerSeq, re.OtherType},
		{re.DominatorID, re.DominatorSeq, re.DominatorType},
		{re.RemoverID, re.RemoverSeq, re.RemoverType},
	} {
		if by.id == nil {
			continue
		}

		ev.ByID = *by.id
		ev.ByType = by.typ

		if by.seq != nil {
			ev.BySeq = *by.seq
		}

		break
	}

	if !ev.Kind.known() {
		unknown["kind="+string(ev.Kind)] = true
	}

	if !knownSrc[ev.Src] {
		unknown["src="+ev.Src] = true
	}

	return ev, nil
}

func convertPath(rp rawPath) (PathNode, *Error) {
	if rp.ID <= 0 {
		return PathNode{}, &Error{Code: CodeInvalid, Field: "path_id", Offset: 0}
	}

	rel, ok := relKey(rp.Rel)
	if !ok {
		return PathNode{}, &Error{Code: CodeInvalid, Field: "rel", Offset: 0}
	}

	p := PathNode{ //nolint:exhaustruct
		ID:        rp.ID,
		PathType:  rp.PathType,
		Rel:       rel,
		SubID:     rp.SubID,
		OuterID:   rp.OuterID,
		InnerID:   rp.InnerID,
		ChildIDs:  rp.ChildIDs,
		Rows:      rp.Rows,
		IndexOID:  rp.IndexOID,
		IndexName: rp.IndexName,
		IndexHypo: rp.IndexHypo,
	}

	for _, c := range rp.ChildIDs {
		if c <= 0 {
			return PathNode{}, &Error{Code: CodeInvalid, Field: "child_path_ids", Offset: 0}
		}
	}

	if rp.RequiredOuter != nil {
		ids, ok := parseRelids(*rp.RequiredOuter)
		if !ok {
			return PathNode{}, &Error{Code: CodeInvalid, Field: "required_outer", Offset: 0}
		}

		p.RequiredOuter = ids
	}

	return p, nil
}

func markSubqueries(paths map[int]PathNode) {
	var walk func(id int)

	walk = func(id int) {
		p, ok := paths[id]
		if !ok || p.InSubquery {
			return
		}

		p.InSubquery = true
		paths[id] = p

		for _, c := range p.Children() {
			walk(c)
		}
	}

	for _, p := range paths {
		if p.PathType == "SubqueryScan" && p.SubID > 0 {
			walk(p.SubID)
		}
	}
}

func convertCoverage(m map[string]bool) Coverage {
	if m == nil {
		return Coverage{} //nolint:exhaustruct
	}

	return Coverage{
		Known:      true,
		Survived:   m["survived_snapshot"],
		Accepted:   m["add_path_accepted"],
		Rejected:   m["add_path_rejected"],
		Displaced:  m["add_path_displaced"],
		Removed:    m["add_path_removed"],
		Precheck:   m["add_path_precheck"],
		Excluded:   m["join_excluded"],
		IndexBuild: m["index_build"],
	}
}

func relKey(s string) (RelKey, bool) {
	if s == string(RelUpper) {
		return RelUpper, true
	}

	if _, ok := parseRelids(s); !ok {
		return "", false
	}

	return RelKey(s), true
}

// allowedFields: encoding/json puts map keys, which are input, into Field.
var allowedFields = fieldNames(rawTrace{}, rawEvent{}, rawPath{}, rawLimits{})

func fieldNames(structs ...any) map[string]bool {
	out := map[string]bool{}

	for _, s := range structs {
		t := reflect.TypeOf(s)
		for i := range t.NumField() {
			if name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ","); name != "" {
				out[name] = true
			}
		}
	}

	return out
}

func decodeError(err error) *Error {
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		return &Error{Code: CodeSyntax, Field: "", Offset: syn.Offset}
	}

	var typ *json.UnmarshalTypeError
	if errors.As(err, &typ) {
		return &Error{Code: CodeType, Field: safeField(typ.Field), Offset: typ.Offset}
	}

	return &Error{Code: CodeSyntax, Field: "", Offset: 0}
}

func safeField(f string) string {
	if f == "" {
		return ""
	}

	parts := strings.Split(f, ".")
	for i, p := range parts {
		if _, err := strconv.Atoi(p); err == nil {
			continue // an array index
		}

		if !allowedFields[p] {
			parts[i] = "*"
		}
	}

	return strings.Join(parts, ".")
}
