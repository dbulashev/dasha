package logs

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// presetPlan is the preset of auto_explain records; Log Insights reads its words.
const presetPlan = "plan"

// searchPreset names the words of which every record the preset is meant for
// holds at least one. A record that holds Substring only inside a longer word
// (deadlock_timeout for deadlock) is not one of them and may be dropped.
type searchPreset struct {
	Substring string
	Words     []string
}

// searchPresets mirrors LOG_PRESETS of the frontend. A preset without words
// narrows nothing beyond its severities.
var searchPresets = map[string]searchPreset{
	"slow":       {Substring: "duration:", Words: []string{"duration"}},
	presetPlan:   {Substring: "plan:", Words: []string{"plan"}},
	"autovacuum": {Substring: "automatic", Words: []string{"automatic"}},
	"deadlock":   {Substring: "deadlock", Words: []string{"deadlock"}},
	// Every LOG message of PostgreSQL holding "point" holds one of these:
	// "recovery restart point at" splits restartpoint in two, the checkpointer
	// names itself when it exits, MultiXact truncation speaks of "checkpointed".
	"checkpoint": {Substring: "point", Words: []string{
		"checkpoint", "checkpoints", "restartpoint", "point", "checkpointer", "checkpointed",
	}},
	"tempFiles":   {Substring: "temporary file", Words: []string{"temporary"}},
	"canceled":    {Substring: "canceling statement", Words: []string{"canceling"}},
	"connections": {},
	"errors":      {},
}

// presetMatch splits the includes of a search by whether they hold the text
// of its preset.
type presetMatch struct {
	// Words go to the store while some include holds the preset text.
	Words []string
	// Preset are the includes holding the preset text, Rest the others.
	Preset, Rest []string
}

// matchPreset finds the preset text among include. The text counts only where
// neither end is glued to a word character: an include edited to
// deadlock_timeout is another search than deadlock, and its records do not
// hold the word deadlock.
func matchPreset(id string, include []string) (presetMatch, error) {
	var m presetMatch

	p, ok := searchPresets[id]
	if id != "" && !ok {
		return m, fmt.Errorf("%w: unknown preset %q", ErrInvalid, id)
	}

	for _, inc := range include {
		if p.Substring != "" && holdsAtBoundary(inc, p.Substring) {
			m.Preset = append(m.Preset, inc)
		} else {
			m.Rest = append(m.Rest, inc)
		}
	}

	if len(m.Preset) > 0 {
		m.Words = p.Words
	}

	return m, nil
}

// holdsAtBoundary reports whether sub occurs in s, case aside, with a non-word
// character or an end of s on both sides.
func holdsAtBoundary(s, sub string) bool {
	s, sub = strings.ToLower(s), strings.ToLower(sub)

	for from := 0; ; {
		i := strings.Index(s[from:], sub)
		if i < 0 {
			return false
		}

		i += from
		end := i + len(sub)
		before, _ := utf8.DecodeLastRuneInString(s[:i])
		after, _ := utf8.DecodeRuneInString(s[end:])

		if (i == 0 || !wordRune(before)) && (end == len(s) || !wordRune(after)) {
			return true
		}

		from = i + 1
	}
}

// wordRune is a character a store may keep inside a token: some tokenizers
// do not split on a dot or an apostrophe between letters.
func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_.'", r)
}
