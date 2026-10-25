package pathprobe

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

func load(t *testing.T, name string) Trace {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}

	tr, err := Parse(body)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}

	return tr
}

func TestParse_DemoJoin(t *testing.T) {
	tr := load(t, "demo-join.json")

	if tr.Rels[1] != "c" || tr.Rels[2] != "o" {
		t.Errorf("rels %v", tr.Rels)
	}

	kinds := map[Kind]int{}
	for _, ev := range tr.Events {
		kinds[ev.Kind]++
	}

	want := map[Kind]int{KindAccepted: 14, KindSkipped: 13, KindRejected: 7, KindDisplaced: 6, KindSurvived: 8}
	for k, n := range want {
		if kinds[k] != n {
			t.Errorf("%s: %d events, want %d", k, kinds[k], n)
		}
	}

	if len(tr.Events) != 48 || len(tr.Paths) != 24 {
		t.Errorf("%d events, %d paths, want 48 and 24", len(tr.Events), len(tr.Paths))
	}

	if !tr.Coverage.Known || !tr.Coverage.Accepted || !tr.Coverage.Removed || tr.Coverage.IndexBuild {
		t.Errorf("coverage %+v", tr.Coverage)
	}

	if len(tr.Unknown) != 0 {
		t.Errorf("unknown %v", tr.Unknown)
	}

	// path 8: the parameterized scan of pp_orders_customer_idx
	p := tr.Paths[8]
	if p.PathType != "IndexScan" || p.Rel != "{2}" || p.IndexName == nil || *p.IndexName != "pp_orders_customer_idx" ||
		p.IndexOID != 16403 || p.Rows == nil || *p.Rows != 100 || !slices.Equal(p.RequiredOuter, []int{1}) {
		t.Errorf("path 8: %+v", p)
	}

	// path 11: a nested loop over it
	if n := tr.Paths[11]; n.OuterID != 2 || n.InnerID != 8 || n.SubID != 0 || !slices.Equal(n.Children(), []int{2, 8}) {
		t.Errorf("path 11 edges: %+v", n)
	}

	// event #26: path 11 displaced by path 15 (#25)
	ev := tr.Events[25]
	if ev.Seq != 26 || ev.Kind != KindDisplaced || ev.PathID != 11 || ev.ByID != 15 || ev.BySeq != 25 ||
		ev.ByType == nil || *ev.ByType != "NestLoop" || ev.CostStartup == nil || *ev.CostStartup != 0.71 {
		t.Errorf("event 26: %+v", ev)
	}

	// event #4: path 4 rejected by path 2, accepted at #2
	ev = tr.Events[3]
	if ev.Kind != KindRejected || ev.PathID != 4 || ev.ByID != 2 || ev.BySeq != 2 ||
		ev.ByType == nil || *ev.ByType != "IndexOnlyScan" || ev.Gap != nil {
		t.Errorf("event 4: %+v", ev)
	}

	// event #17: a skipped nested loop has no path
	if ev := tr.Events[16]; ev.Kind != KindSkipped || ev.PathID != 0 || ev.SkipReason == nil || ev.CostTotal == nil {
		t.Errorf("event 17: %+v", ev)
	}

	// event #48 is in an upper rel
	if ev := tr.Events[47]; !ev.Rel.IsUpper() || ev.Rel.Members() != nil {
		t.Errorf("event 48 rel %q", ev.Rel)
	}

	// event #8: a real survivor kept for its startup cost
	if ev := tr.Events[7]; ev.KeptReason == nil || *ev.KeptReason != "startup" || ev.Label != nil || ev.Origin != "real" {
		t.Errorf("event 8: %+v", ev)
	}

	for _, p := range tr.Paths {
		if p.InSubquery {
			t.Errorf("path %d in a subquery", p.ID)
		}
	}
}

func TestParse_Removed(t *testing.T) {
	tr := load(t, "removed.json")

	ev := tr.Events[7]
	if ev.Kind != KindRemoved || ev.PathID != 2 || ev.ByID != 4 || ev.BySeq != 7 || ev.ByType == nil || *ev.ByType != "CustomScan" {
		t.Errorf("event 8: %+v", ev)
	}

	if len(tr.Unknown) != 0 {
		t.Errorf("unknown %v", tr.Unknown)
	}

	// pathprobe.min_gap drops the remover's rejected event: remover_seq is null
	tr2, err := Parse([]byte(`{"events": [{"seq": 8, "src": "wp", "rel": "{1}", "kind": "removed", "path_id": 2,
		"remover_path_id": 4, "remover_seq": null, "remover_pathtype": "CustomScan"}]}`))
	if err != nil {
		t.Fatal(err)
	}

	if ev := tr2.Events[0]; ev.ByID != 4 || ev.BySeq != 0 || ev.CostTotal != nil {
		t.Errorf("removed without remover_seq: %+v", ev)
	}
}

func TestParse_ChildPathIDs(t *testing.T) {
	tr := load(t, "bitmap-or.json")

	or := tr.Paths[3]
	if !slices.Equal(or.ChildIDs, []int{4, 5}) || !slices.Equal(or.Children(), []int{4, 5}) || or.Rows != nil {
		t.Errorf("BitmapOr: %+v", or)
	}

	if heap := tr.Paths[2]; heap.SubID != 3 || heap.IndexName != nil {
		t.Errorf("BitmapHeapScan over BitmapOr: %+v", heap)
	}
}

func TestParse_Subquery(t *testing.T) {
	tr := load(t, "setop.json")

	if !slices.Equal(tr.Paths[5].ChildIDs, []int{3, 4}) {
		t.Errorf("SetOp children %v", tr.Paths[5].ChildIDs)
	}

	for id, want := range map[int]bool{1: true, 2: true, 3: false, 4: false, 5: false} {
		if tr.Paths[id].InSubquery != want {
			t.Errorf("path %d in subquery: %t, want %t", id, tr.Paths[id].InSubquery, want)
		}
	}
}

func TestParse_Limits(t *testing.T) {
	if tr := load(t, "truncated.json"); !tr.Limits.EventsTruncated || tr.Limits.MaxEvents != 16 {
		t.Errorf("limits %+v", tr.Limits)
	}
}

func TestParse_Errors(t *testing.T) {
	cases := []struct {
		name, body, code, field string
	}{
		{"not json", `{"version": "1.1", "events": [`, CodeSyntax, ""},
		{"garbage", `SELECT 1`, CodeSyntax, ""},
		{"no events", `{"version": "1.1"}`, CodeNotPathprobe, "events"},
		{"bad rel", `{"events": [{"seq": 1, "rel": "{x}", "kind": "accepted"}]}`, CodeInvalid, "events[0].rel"},
		{"bad rels key", `{"rels": {"zero": "t"}, "events": []}`, CodeInvalid, "rels"},
		{"wrong type", `{"events": [{"seq": "one"}]}`, CodeType, "events.0.seq"},
		{"dup path", `{"events": [], "paths": [{"path_id": 1, "rel": "{1}"}, {"path_id": 1, "rel": "{1}"}]}`, CodeInvalid, "paths[1].path_id"},
		{"bad child", `{"events": [], "paths": [{"path_id": 1, "rel": "{1}", "child_path_ids": [0]}]}`, CodeInvalid, "paths[0].child_path_ids"},
		{"self subpath", `{"events": [], "paths": [{"path_id": 1, "rel": "{1}", "pathtype": "SubqueryScan", "subpath_id": 1}]}`, CodeInvalid, "paths[0]"},
		{"child cycle", `{"events": [], "paths": [
			{"path_id": 1, "rel": "{1}", "pathtype": "SeqScan"},
			{"path_id": 2, "rel": "{1}", "pathtype": "BitmapOr", "child_path_ids": [3]},
			{"path_id": 3, "rel": "{1}", "pathtype": "BitmapOr", "child_path_ids": [2]}]}`, CodeInvalid, "paths[1]"},
		{"join cycle", `{"events": [], "paths": [
			{"path_id": 1, "rel": "{1,2}", "pathtype": "NestLoop", "outer_path_id": 2, "inner_path_id": 3},
			{"path_id": 2, "rel": "{1}", "pathtype": "SeqScan"},
			{"path_id": 3, "rel": "{2}", "pathtype": "Material", "subpath_id": 1}]}`, CodeInvalid, "paths[0]"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.body))

			var pe *Error
			if !errors.As(err, &pe) {
				t.Fatalf("error %v, want *Error", err)
			}

			if pe.Code != c.code || pe.Field != c.field {
				t.Errorf("got %s at %q, want %s at %q", pe.Code, pe.Field, c.code, c.field)
			}
		})
	}
}

// TestParse_SharedChild: a path under several parents is not a cycle; a missing child is skipped.
func TestParse_SharedChild(t *testing.T) {
	body := `{"events": [], "paths": [
		{"path_id": 1, "rel": "{1,2}", "pathtype": "NestLoop", "outer_path_id": 3, "inner_path_id": 4},
		{"path_id": 2, "rel": "{1,2}", "pathtype": "HashJoin", "outer_path_id": 3, "inner_path_id": 4},
		{"path_id": 3, "rel": "{1}", "pathtype": "SeqScan"},
		{"path_id": 4, "rel": "{2}", "pathtype": "SeqScan", "subpath_id": 9}]}`

	tr, err := Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}

	if len(tr.Paths) != 4 {
		t.Errorf("paths %d, want 4", len(tr.Paths))
	}
}

// TestParse_ErrorHidesInput: encoding/json names map keys in Field.
func TestParse_ErrorHidesInput(t *testing.T) {
	_, err := Parse([]byte(`{"rels": {"secret_alias": 5}, "events": []}`))
	if err == nil {
		t.Fatal("no error")
	}

	if !errorShape.MatchString(err.Error()) || regexp.MustCompile(`secret`).MatchString(err.Error()) {
		t.Errorf("error %q leaks input", err.Error())
	}
}

var errorShape = regexp.MustCompile(`^pathprobe trace: [a-z_]+( at [a-z_\[\]0-9.*]+)?( offset \d+)?$`)

func FuzzParse(f *testing.F) {
	for _, name := range []string{"demo-join.json", "removed.json", "setop.json", "truncated.json"} {
		body, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			f.Fatal(err)
		}

		f.Add(body)
	}

	f.Add([]byte(`{"rels": {"x": "y"}, "events": [{"rel": "{1,"}]}`))
	f.Add([]byte(`{"events": [], "paths": [{"path_id": 1, "rel": "{1}", "pathtype": "SubqueryScan", "subpath_id": 1}]}`))

	f.Fuzz(func(t *testing.T, body []byte) {
		_, err := Parse(body)
		if err != nil && !errorShape.MatchString(err.Error()) {
			t.Errorf("error %q is not of the allowed shape", err.Error())
		}
	})
}
