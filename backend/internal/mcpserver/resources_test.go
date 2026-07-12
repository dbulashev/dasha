package mcpserver

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

var kbResourceNames = []string{"health-rules", "wait-events", "workflow"}

func TestKBHandler_ServesMarkdown(t *testing.T) {
	t.Parallel()

	cyrillic := regexp.MustCompile(`[а-яА-Я]`)

	for _, lang := range kbLangs {
		for _, name := range kbResourceNames {
			uri := "dasha://kb/" + name

			res, err := kbHandler(uri, lang+"/"+name+".md")(context.Background(), nil)
			if err != nil {
				t.Fatalf("kbHandler(%s, %s): %v", uri, lang, err)
			}

			if len(res.Contents) != 1 {
				t.Fatalf("kbHandler(%s, %s): %d contents, want 1", uri, lang, len(res.Contents))
			}

			c := res.Contents[0]
			if c.URI != uri || c.MIMEType != "text/markdown" || c.Text == "" {
				t.Errorf("kbHandler(%s, %s) = {URI:%q MIME:%q len:%d}, want matching URI, markdown, non-empty",
					uri, lang, c.URI, c.MIMEType, len(c.Text))
			}

			if isRu := cyrillic.MatchString(c.Text); isRu != (lang == "ru") {
				t.Errorf("kbHandler(%s, %s): cyrillic=%v does not match the language", uri, lang, isRu)
			}
		}
	}
}

func TestKBHandler_UnknownPathErrors(t *testing.T) {
	t.Parallel()

	if _, err := kbHandler("dasha://kb/nope", "en/nope.md")(context.Background(), nil); err == nil {
		t.Errorf("kbHandler(unknown file) must return an error")
	}
}

func TestValidLang(t *testing.T) {
	t.Parallel()

	for lang, want := range map[string]bool{"en": true, "ru": true, "": false, "de": false} {
		if validLang(lang) != want {
			t.Errorf("validLang(%q) = %v, want %v", lang, !want, want)
		}
	}
}

func TestTextsFor_FallbackAndCompleteness(t *testing.T) {
	t.Parallel()

	if textsFor("xx") != texts["en"] {
		t.Errorf("textsFor(unknown) must fall back to English")
	}

	for lang, want := range texts {
		got := textsFor(lang)
		if got != want {
			t.Errorf("textsFor(%q) returned the wrong set", lang)
		}

		// Every text of every language must be present and reference the
		// knowledge base where the playbook relies on it.
		for name, s := range map[string]string{
			"instructions": got.instructions,
			"diagnose":     got.diagnose,
			"explain":      got.explain,
			"indexes":      got.indexes,
			"slowQueries":  got.slowQueries,
			"fleet":        got.fleet,
		} {
			if s == "" {
				t.Errorf("texts[%q].%s is empty", lang, name)
			}

			if name != "instructions" && !strings.Contains(s, "dasha://kb/") {
				t.Errorf("texts[%q].%s does not point at any dasha://kb resource", lang, name)
			}
		}
	}
}
