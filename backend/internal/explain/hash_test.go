package explain

import "testing"

// Two runs of the same statement with different literals are one plan shape.
func TestHash_IgnoresLiterals(t *testing.T) {
	for _, version := range versions {
		t.Run(version, func(t *testing.T) {
			first := parseFixture(t, version+"/param_1.txt", SourceLog)
			second := parseFixture(t, version+"/param_2.txt", SourceLog)

			if first.Root.Filter == second.Root.Filter && first.QueryText == second.QueryText {
				t.Fatal("fixtures are meant to differ in their literals")
			}

			if got, want := Hash(first.Root), Hash(second.Root); got != want {
				t.Errorf("hash differs on literals alone: %s vs %s", got, want)
			}
		})
	}
}

func TestHash_DistinguishesShape(t *testing.T) {
	nested := parseFixture(t, "synthetic/nested_loop.txt", SourceLog)
	loops := parseFixture(t, "synthetic/loops_blowup.txt", SourceLog)

	if Hash(nested.Root) == Hash(loops.Root) {
		t.Error("different trees share a hash")
	}
}

func TestHash_DistinguishesIndex(t *testing.T) {
	base := parseFixture(t, "synthetic/loops_blowup.txt", SourceLog)

	renamed := base
	renamed.Root = *cloneNode(&base.Root)
	renamed.Root.Children[1].IndexName = "b_other_idx"

	if Hash(base.Root) == Hash(renamed.Root) {
		t.Error("the index a scan uses is part of the shape")
	}
}

func TestHash_IgnoresCostsAndActuals(t *testing.T) {
	base := parseFixture(t, "synthetic/loops_blowup.txt", SourceLog)

	changed := base
	changed.Root = *cloneNode(&base.Root)
	changed.Root.TotalCost = 999999
	changed.Root.PlanRows = 1
	changed.Root.Children[0].Filter = "(other)"
	changed.Root.Children[1].Actual = &Actual{Rows: 42, Loops: 42}

	if Hash(base.Root) != Hash(changed.Root) {
		t.Error("costs, estimates and conditions must stay out of the hash")
	}
}

func TestHash_Format(t *testing.T) {
	p := parseFixture(t, "synthetic/loops_blowup.txt", SourceLog)

	h := Hash(p.Root)
	if len(h) != 16 {
		t.Fatalf("want 16 hex characters, got %q", h)
	}

	for _, r := range h {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			t.Fatalf("not hex: %q", h)
		}
	}
}

func cloneNode(n *Node) *Node {
	out := *n
	out.Children = make([]Node, len(n.Children))

	for i := range n.Children {
		out.Children[i] = *cloneNode(&n.Children[i])
	}

	return &out
}
