package logs

import "fmt"

// presetPlan is the preset of auto_explain records; Log Insights reads its word.
const presetPlan = "plan"

// searchPreset names the word every record holding Substring holds.
type searchPreset struct {
	Substring string
	Word      string
}

// searchPresets mirrors LOG_PRESETS of the frontend. A preset without a word
// narrows nothing beyond its severities.
var searchPresets = map[string]searchPreset{
	"slow":        {Substring: "duration:", Word: "duration"},
	presetPlan:    {Substring: "plan:", Word: "plan"},
	"autovacuum":  {},
	"deadlock":    {},
	"checkpoint":  {},
	"tempFiles":   {},
	"canceled":    {},
	"connections": {},
	"errors":      {},
}

// presetWord is the word of preset id while some include filter still contains
// its substring; an edited preset text sends no word.
func presetWord(id string, include []string) (string, error) {
	if id == "" {
		return "", nil
	}

	p, ok := searchPresets[id]
	if !ok {
		return "", fmt.Errorf("%w: unknown preset %q", ErrInvalid, id)
	}

	if p.Word == "" {
		return "", nil
	}

	for _, inc := range include {
		if containsFold(inc, p.Substring) {
			return p.Word, nil
		}
	}

	return "", nil
}
