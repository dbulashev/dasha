package mcpserver

// Importing internal/health is allowed only here: a _test.go import is never
// linked into cmd/dasha-mcp. Threshold values inside rule closures stay
// unchecked — changing them must touch kb too.

import (
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/dbulashev/dasha/internal/health"
)

// maxKBFileBytes caps one knowledge-base file so reading a resource cannot
// flood a small model's context.
const maxKBFileBytes = 24 * 1024

var kbLangs = []string{"en", "ru"}

func TestKB_CoversEveryHealthRule(t *testing.T) {
	t.Parallel()

	heading := regexp.MustCompile(`(?m)^### ([a-z0-9_]+)$`)

	for _, lang := range kbLangs {
		path := "kb/" + lang + "/health-rules.md"

		b, err := kbFS.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		found := map[string]bool{}
		for _, m := range heading.FindAllStringSubmatch(string(b), -1) {
			found[m[1]] = true
		}

		known := map[string]bool{}

		for _, r := range health.Registry {
			known[r.ID] = true

			if !found[r.ID] {
				t.Errorf("%s: rule %q from health.Registry has no '### %s' section", path, r.ID, r.ID)
			}
		}

		for id := range found {
			if !known[id] {
				t.Errorf("%s: section %q matches no Registry rule — stale entry?", path, id)
			}
		}
	}
}

// TestHealthDetails_RuleAliasesExist keeps the health_details drill-down table in
// lockstep with the rules engine. Each entry advertises the rule_ids it explains,
// and the tool accepts those as aliases; if a rule is renamed or dropped, the alias
// would silently stop resolving and the model's "pass the rule_id back" flow would
// break with an unknown-detail error. The duplicate check matters too: a rule
// aliased by two details would be silently overwritten when the index is built.
func TestHealthDetails_RuleAliasesExist(t *testing.T) {
	t.Parallel()

	known := make(map[string]bool, len(health.Registry))
	for _, r := range health.Registry {
		known[r.ID] = true
	}

	claimedBy := map[string]string{}

	for _, d := range healthDetailList {
		for _, rule := range d.rules {
			if !known[rule] {
				t.Errorf("health_details %q aliases rule %q, which is not in health.Registry — renamed or removed?", d.name, rule)
			}

			if prev, dup := claimedBy[rule]; dup {
				t.Errorf("rule %q is aliased by both %q and %q — the lookup index would keep only one", rule, prev, d.name)
			}

			claimedBy[rule] = d.name
		}
	}
}

func TestKB_SizeAndLanguageParity(t *testing.T) {
	t.Parallel()

	files := map[string][]string{}

	for _, lang := range kbLangs {
		entries, err := fs.ReadDir(kbFS, "kb/"+lang)
		if err != nil {
			t.Fatalf("read kb/%s: %v", lang, err)
		}

		for _, e := range entries {
			files[lang] = append(files[lang], e.Name())

			info, err := e.Info()
			if err != nil {
				t.Fatalf("stat kb/%s/%s: %v", lang, e.Name(), err)
			}

			if info.Size() == 0 {
				t.Errorf("kb/%s/%s is empty", lang, e.Name())
			}

			if info.Size() > maxKBFileBytes {
				t.Errorf("kb/%s/%s is %d bytes, exceeds the %d context budget", lang, e.Name(), info.Size(), maxKBFileBytes)
			}
		}
	}

	// Every language must ship the same set of files.
	if len(files["en"]) == 0 {
		t.Fatalf("no kb files embedded for en")
	}

	for _, lang := range kbLangs[1:] {
		if len(files[lang]) != len(files["en"]) {
			t.Fatalf("kb/%s has %d files, kb/en has %d — languages must stay symmetric", lang, len(files[lang]), len(files["en"]))
		}

		for i, name := range files["en"] {
			if files[lang][i] != name {
				t.Errorf("kb/%s file %q does not match kb/en file %q", lang, files[lang][i], name)
			}
		}
	}

	// Heading text cannot be compared across translations — the nesting can.
	for _, name := range files["en"] {
		want := headingLevels(t, "kb/en/"+name)

		for _, lang := range kbLangs[1:] {
			if got := headingLevels(t, "kb/"+lang+"/"+name); !slices.Equal(got, want) {
				t.Errorf("kb/%s/%s has heading structure %v, kb/en/%s has %v — a section is missing or extra",
					lang, name, got, name, want)
			}
		}
	}
}

// headingLevels returns the depth of each markdown heading in order (2 for "##",
// 3 for "###"), which is a translation-independent fingerprint of a file's shape.
func headingLevels(t *testing.T, path string) []int {
	t.Helper()

	b, err := kbFS.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var levels []int

	for _, line := range strings.Split(string(b), "\n") {
		n := 0
		for n < len(line) && line[n] == '#' {
			n++
		}

		if n > 0 && n < len(line) && line[n] == ' ' {
			levels = append(levels, n)
		}
	}

	return levels
}
