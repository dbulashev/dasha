package mcpserver

// This file deliberately imports internal/health — the only place in mcpserver
// allowed to: the runtime boundary ("depend only on gen/apiclient") holds
// because a _test.go import is never linked into cmd/dasha-mcp. The import is
// the mechanism keeping the embedded knowledge base in lockstep with the rules
// engine: a rule added to (or removed from) health.Registry without a matching
// kb section fails CI. Threshold VALUES inside rule closures are not checkable
// this way — reviews changing severityFor(...) numbers must touch kb too.

import (
	"io/fs"
	"regexp"
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
}
