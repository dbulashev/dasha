package http

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dbulashev/dasha/internal/explain"
)

func TestClipKeepsRunesWholeAndCountsTheRest(t *testing.T) {
	t.Parallel()

	if got, omitted := clip("short", 16); got != "short" || omitted != nil {
		t.Errorf("clip(short) = %q, %v; want it untouched", got, omitted)
	}

	s := strings.Repeat("я", 10) // 20 bytes

	got, omitted := clip(s, 5)
	if !utf8.ValidString(got) || len(got) != 4 {
		t.Errorf("clip = %q (%d bytes), want 4 bytes of whole runes", got, len(got))
	}

	if omitted == nil || *omitted != 16 {
		t.Errorf("omitted = %v, want 16", omitted)
	}
}

func TestMapPlanSummaryClipsLongTexts(t *testing.T) {
	t.Parallel()

	list := strings.Repeat("1,", 20000)

	p := explain.Plan{
		Format:    explain.FormatText,
		QueryText: "SELECT count(*) FROM t WHERE id = ANY ('{" + list + "}')",
		Root: explain.Node{
			Type:     "Seq Scan",
			Relation: "t",
			Filter:   "(id = ANY ('{" + list + "}'::integer[]))",
			Children: []explain.Node{{Type: "Result", Filter: "(true)"}},
		},
	}

	out := mapPlanSummary(&p, nil, nil)

	if len(out.QueryText) > planQueryTextLimit || out.QueryTextOmittedBytes == nil ||
		len(out.QueryText)+*out.QueryTextOmittedBytes != len(p.QueryText) {
		t.Errorf("query text %d bytes, omitted %v; want the head and the rest counted",
			len(out.QueryText), out.QueryTextOmittedBytes)
	}

	if out.QueryParams != nil || out.QueryParamsOmittedBytes != nil {
		t.Error("query params appeared on a plan that has none")
	}

	root := out.Root
	if root.Filter == nil || len(*root.Filter) > planConditionLimit || root.FilterOmittedBytes == nil {
		t.Errorf("root filter not clipped: %d bytes, omitted %v", len(deref(root.Filter)), root.FilterOmittedBytes)
	}

	if child := out.Root.Children[0]; deref(child.Filter) != "(true)" || child.FilterOmittedBytes != nil {
		t.Errorf("short child filter changed: %q, omitted %v", deref(child.Filter), child.FilterOmittedBytes)
	}

	if root.IndexCond != nil || root.IndexCondOmittedBytes != nil {
		t.Error("index cond appeared on a node that has none")
	}
}
