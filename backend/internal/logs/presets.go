package logs

import "fmt"

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

// presetWords are the words of preset id while some include filter still
// contains its substring; an edited preset text sends none.
func presetWords(id string, include []string) ([]string, error) {
	if id == "" {
		return nil, nil
	}

	p, ok := searchPresets[id]
	if !ok {
		return nil, fmt.Errorf("%w: unknown preset %q", ErrInvalid, id)
	}

	if len(p.Words) == 0 {
		return nil, nil
	}

	for _, inc := range include {
		if containsFold(inc, p.Substring) {
			return p.Words, nil
		}
	}

	return nil, nil
}
